// Package ratecache is an in-memory, partial-coverage cache for published
// tariff price series. Historical prices are immutable public data, so
// caching them server-side keeps the app's "no user data stored" guarantee
// intact while sparing Octopus repeated full-range fetches: a request that
// partially overlaps the cached envelope only fetches the missing head
// and/or tail.
//
// The cache is process-local by design — on Fly with scale-to-zero it dies
// with the machine and warms again on first use, which is fine for this
// traffic.
package ratecache

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

// Fetcher is the upstream the cache fills from; the octopus client
// implements it.
type Fetcher interface {
	RateSeries(ctx context.Context, product, region, leaf string, from, to time.Time) ([]octopus.RatePoint, error)
}

// maxEntries caps the cache. A year of half-hourly prices is ~17.5k points
// (~1 MB); 64 keys covers every GSP region for several products with
// bounded memory.
const maxEntries = 64

type key struct {
	product, region, leaf string
}

// entry holds one key's points plus the time envelope [from, to) the cache
// has actually covered. Points beyond the envelope may exist (an open-ended
// standing charge, prices published ahead of time) but are not trusted as
// complete until a fetch covers them.
type entry struct {
	from     time.Time
	to       time.Time
	lastUsed time.Time
	points   []octopus.RatePoint
}

// Cache wraps a Fetcher with partial-coverage memoisation. Safe for
// concurrent use; the single mutex is held across upstream fetches, which
// serialises them — simple and correct, and fine at this traffic level.
type Cache struct {
	fetcher Fetcher
	entries map[key]*entry
	now     func() time.Time
	mu      sync.Mutex
}

// New returns an empty cache filling from fetcher.
func New(fetcher Fetcher) *Cache {
	return &Cache{
		fetcher: fetcher,
		entries: make(map[key]*entry),
		now:     time.Now,
	}
}

// RateSeries returns the price points overlapping [from, to), fetching only
// the spans the cache has not covered yet. It satisfies the same interface
// as the underlying client, so handlers use it as a drop-in.
func (c *Cache) RateSeries(ctx context.Context, product, region, leaf string, from, to time.Time) ([]octopus.RatePoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := key{product: product, region: region, leaf: leaf}
	now := c.now()
	e := c.entries[k]

	// Disjoint request (a gap would sit between the cached envelope and the
	// requested range): bridging could mean fetching months nobody asked
	// for, so replace the entry with the requested range instead.
	if e != nil && (to.Before(e.from) || from.After(e.to)) {
		delete(c.entries, k)
		e = nil
	}

	if e == nil {
		points, err := c.fetcher.RateSeries(ctx, product, region, leaf, from, to)
		if err != nil {
			return nil, err //nolint:wrapcheck // transparent passthrough of the client's contextual error
		}
		e = &entry{from: from, to: clampToNow(to, now), points: points}
		c.entries[k] = e
		c.evictIfFull(k)
	} else {
		if err := c.extend(ctx, k, e, from, to, now); err != nil {
			return nil, err
		}
	}

	e.lastUsed = now
	return filterPoints(e.points, from, to), nil
}

// extend fetches the uncovered head and/or tail of [from, to) and merges
// them into the entry. Each successful fetch is merged immediately, so a
// later failure still leaves the cache consistent and improved.
func (c *Cache) extend(ctx context.Context, k key, e *entry, from, to, now time.Time) error {
	if from.Before(e.from) {
		head, err := c.fetcher.RateSeries(ctx, k.product, k.region, k.leaf, from, e.from)
		if err != nil {
			return err //nolint:wrapcheck // transparent passthrough
		}
		e.points = mergePoints(e.points, head)
		e.from = from
	}

	// Fetch whenever the request reaches past the covered envelope — but
	// coverage itself never extends beyond now: the tail near (and after)
	// the present is still being published, so a later request re-fetches
	// that fresh edge.
	if to.After(e.to) {
		tail, err := c.fetcher.RateSeries(ctx, k.product, k.region, k.leaf, e.to, to)
		if err != nil {
			return err //nolint:wrapcheck // transparent passthrough
		}
		e.points = mergePoints(e.points, tail)
		if covered := clampToNow(to, now); covered.After(e.to) {
			e.to = covered
		}
	}
	return nil
}

func clampToNow(to, now time.Time) time.Time {
	if to.After(now) {
		return now
	}
	return to
}

// mergePoints combines two fetches, deduplicating by interval start with
// the newer fetch winning (an interval that was open-ended when first
// cached may have gained a valid_to since). Result is sorted by From.
func mergePoints(existing, fresh []octopus.RatePoint) []octopus.RatePoint {
	byStart := make(map[int64]octopus.RatePoint, len(existing)+len(fresh))
	for _, p := range existing {
		byStart[p.From.UnixMilli()] = p
	}
	for _, p := range fresh {
		byStart[p.From.UnixMilli()] = p
	}
	merged := make([]octopus.RatePoint, 0, len(byStart))
	for _, p := range byStart {
		merged = append(merged, p)
	}
	slices.SortFunc(merged, func(a, b octopus.RatePoint) int {
		return a.From.Compare(b.From)
	})
	return merged
}

// filterPoints returns a fresh slice of the points overlapping [from, to).
func filterPoints(points []octopus.RatePoint, from, to time.Time) []octopus.RatePoint {
	out := make([]octopus.RatePoint, 0, len(points))
	for _, p := range points {
		if !p.From.Before(to) {
			continue
		}
		if p.To != nil && !p.To.After(from) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// evictIfFull drops the least-recently-used entry (never the one just
// inserted) once the cache exceeds its cap.
func (c *Cache) evictIfFull(justInserted key) {
	if len(c.entries) <= maxEntries {
		return
	}
	var oldest key
	var oldestUsed time.Time
	first := true
	for k, e := range c.entries {
		if k == justInserted {
			continue
		}
		if first || e.lastUsed.Before(oldestUsed) {
			oldest, oldestUsed, first = k, e.lastUsed, false
		}
	}
	delete(c.entries, oldest)
}
