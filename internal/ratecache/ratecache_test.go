package ratecache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

// fakeFetcher records every upstream call and serves synthetic half-hourly
// points covering exactly the requested range.
type fakeFetcher struct {
	err   error
	calls []fetchCall
	rate  float64
}

type fetchCall struct {
	from    time.Time
	to      time.Time
	product string
	region  string
	leaf    string
}

func (f *fakeFetcher) RateSeries(_ context.Context, product, region, leaf string, from, to time.Time) ([]octopus.RatePoint, error) {
	f.calls = append(f.calls, fetchCall{product: product, region: region, leaf: leaf, from: from, to: to})
	if f.err != nil {
		return nil, f.err
	}
	var points []octopus.RatePoint
	for t := from; t.Before(to); t = t.Add(30 * time.Minute) {
		end := t.Add(30 * time.Minute)
		points = append(points, octopus.RatePoint{From: t, To: &end, Rate: f.rate})
	}
	return points, nil
}

var fixedNow = time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

func day(d int) time.Time {
	return time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC)
}

func newTestCache(f Fetcher) *Cache {
	c := New(f)
	c.now = func() time.Time { return fixedNow }
	return c
}

func mustSeries(t *testing.T, c *Cache, from, to time.Time) []octopus.RatePoint {
	t.Helper()
	points, err := c.RateSeries(t.Context(), "AGILE-24-10-01", "C", "standard-unit-rates", from, to)
	if err != nil {
		t.Fatalf("RateSeries: %v", err)
	}
	return points
}

func TestCacheMissThenHit(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	first := mustSeries(t, c, day(1), day(3))
	if len(first) != 96 {
		t.Fatalf("got %d points, want 96 (2 days half-hourly)", len(first))
	}
	if len(f.calls) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(f.calls))
	}

	second := mustSeries(t, c, day(1), day(3))
	if len(f.calls) != 1 {
		t.Errorf("upstream calls after repeat = %d, want still 1 (cache hit)", len(f.calls))
	}
	if len(second) != 96 {
		t.Errorf("repeat returned %d points, want 96", len(second))
	}
}

func TestCacheSubRangeServedFromMemory(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	mustSeries(t, c, day(1), day(5))
	inner := mustSeries(t, c, day(2), day(3))
	if len(f.calls) != 1 {
		t.Errorf("upstream calls = %d, want 1 (sub-range fully covered)", len(f.calls))
	}
	if len(inner) != 48 {
		t.Errorf("inner range returned %d points, want 48", len(inner))
	}
	for _, p := range inner {
		if p.From.Before(day(2)) || !p.From.Before(day(3)) {
			t.Errorf("point at %v outside requested sub-range", p.From)
		}
	}
}

func TestCacheExtendsTailOnly(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	mustSeries(t, c, day(1), day(3))
	points := mustSeries(t, c, day(2), day(5))

	if len(f.calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(f.calls))
	}
	tail := f.calls[1]
	if !tail.from.Equal(day(3)) || !tail.to.Equal(day(5)) {
		t.Errorf("tail fetch = [%v, %v), want [day3, day5) — only the missing span", tail.from, tail.to)
	}
	if len(points) != 144 {
		t.Errorf("got %d points, want 144 (3 days)", len(points))
	}
}

func TestCacheExtendsHeadOnly(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	mustSeries(t, c, day(3), day(5))
	mustSeries(t, c, day(1), day(4))

	if len(f.calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(f.calls))
	}
	head := f.calls[1]
	if !head.from.Equal(day(1)) || !head.to.Equal(day(3)) {
		t.Errorf("head fetch = [%v, %v), want [day1, day3)", head.from, head.to)
	}
}

func TestCacheExtendsBothEnds(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	mustSeries(t, c, day(2), day(3))
	points := mustSeries(t, c, day(1), day(5))

	if len(f.calls) != 3 {
		t.Fatalf("upstream calls = %d, want 3 (initial + head + tail)", len(f.calls))
	}
	if len(points) != 192 {
		t.Errorf("got %d points, want 192 (4 days)", len(points))
	}
	// No duplicates after merging.
	seen := map[int64]bool{}
	for _, p := range points {
		if seen[p.From.UnixMilli()] {
			t.Fatalf("duplicate point at %v after merge", p.From)
		}
		seen[p.From.UnixMilli()] = true
	}
}

func TestCacheDisjointRangeReplacesEntry(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	mustSeries(t, c, day(1), day(2))
	mustSeries(t, c, day(8), day(9)) // disjoint: would need a 6-day bridge

	if len(f.calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(f.calls))
	}
	second := f.calls[1]
	if !second.from.Equal(day(8)) || !second.to.Equal(day(9)) {
		t.Errorf("disjoint fetch = [%v, %v), want exactly the requested range, no bridge", second.from, second.to)
	}

	// The old range was dropped; asking for it again re-fetches.
	mustSeries(t, c, day(1), day(2))
	if len(f.calls) != 3 {
		t.Errorf("upstream calls = %d, want 3 (old entry replaced)", len(f.calls))
	}
}

func TestCacheNeverTrustsTheFutureEdge(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	// Request ends after "now": coverage must clamp at now, so a repeat
	// request re-fetches the still-being-published tail.
	endOfToday := fixedNow.Add(12 * time.Hour)
	mustSeries(t, c, day(9), endOfToday)
	mustSeries(t, c, day(9), endOfToday)

	if len(f.calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2 (tail beyond now is never marked covered)", len(f.calls))
	}
	tail := f.calls[1]
	if !tail.from.Equal(fixedNow) {
		t.Errorf("refetch starts at %v, want now (covered part not re-fetched)", tail.from)
	}
}

func TestCacheKeysAreIndependent(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)

	if _, err := c.RateSeries(t.Context(), "AGILE-24-10-01", "C", "standard-unit-rates", day(1), day(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RateSeries(t.Context(), "AGILE-24-10-01", "H", "standard-unit-rates", day(1), day(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RateSeries(t.Context(), "AGILE-24-10-01", "C", "standing-charges", day(1), day(2)); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 {
		t.Errorf("upstream calls = %d, want 3 (region and kind are separate keys)", len(f.calls))
	}
}

func TestCacheUpstreamErrorPropagatesAndNothingIsCached(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{err: fmt.Errorf("octopus returned status 503")}
	c := newTestCache(f)

	if _, err := c.RateSeries(t.Context(), "AGILE-24-10-01", "C", "standard-unit-rates", day(1), day(2)); err == nil {
		t.Fatal("want error from upstream")
	}

	f.err = nil
	f.rate = 10
	points := mustSeries(t, c, day(1), day(2))
	if len(points) != 48 {
		t.Errorf("got %d points after recovery, want 48", len(points))
	}
	if len(f.calls) != 2 {
		t.Errorf("upstream calls = %d, want 2 (failed call not cached)", len(f.calls))
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{rate: 10}
	c := newTestCache(f)
	// Distinct lastUsed per insert.
	tick := fixedNow
	c.now = func() time.Time { tick = tick.Add(time.Second); return tick }

	products := make([]string, 0, maxEntries+1)
	for i := 0; i <= maxEntries; i++ {
		products = append(products, fmt.Sprintf("PROD-%03d", i))
	}
	for _, prod := range products {
		if _, err := c.RateSeries(t.Context(), prod, "C", "standard-unit-rates", day(1), day(2)); err != nil {
			t.Fatal(err)
		}
	}

	calls := len(f.calls)
	// The first-inserted product was evicted; re-requesting it fetches again.
	if _, err := c.RateSeries(t.Context(), products[0], "C", "standard-unit-rates", day(1), day(2)); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != calls+1 {
		t.Errorf("upstream calls = %d, want %d (evicted entry re-fetched)", len(f.calls), calls+1)
	}
	// A recently used one is still cached.
	if _, err := c.RateSeries(t.Context(), products[maxEntries], "C", "standard-unit-rates", day(1), day(2)); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != calls+1 {
		t.Errorf("upstream calls = %d, want %d (recent entry still cached)", len(f.calls), calls+1)
	}
}
