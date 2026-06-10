package costing

import "fmt"

// BandsFromRates reverse-engineers a tariff's band structure from a resolved
// per-bucket rate table (the inverse of costStream's rate resolution). The
// most common rate becomes the default; maximal runs of any other rate become
// bands, and a run crossing midnight collapses into a single wrap band
// (from > to). Used to prefill a tariff from the rates Octopus publishes for
// the user's current product.
func BandsFromRates(rates *[BucketsPerDay]float64) (def float64, bands []Band) {
	def = modalRate(rates)
	runs := nonDefaultRuns(rates, def)

	// A run ending at the last bucket and one starting at bucket 0 with the
	// same rate are one band that wraps midnight.
	if len(runs) > 1 {
		first, last := runs[0], runs[len(runs)-1]
		if first.start == 0 && last.end == BucketsPerDay-1 && first.rate == last.rate {
			runs[len(runs)-1].end = first.end
			runs = runs[1:]
		}
	}

	for _, r := range runs {
		bands = append(bands, Band{
			From: bucketLabel(r.start),
			To:   bucketLabel((r.end + 1) % BucketsPerDay),
			Rate: r.rate,
		})
	}
	return def, bands
}

// bucketRun is a maximal stretch of consecutive buckets sharing a
// non-default rate; end is inclusive.
type bucketRun struct {
	start, end int
	rate       float64
}

func nonDefaultRuns(rates *[BucketsPerDay]float64, def float64) []bucketRun {
	var runs []bucketRun
	for i := 0; i < BucketsPerDay; i++ {
		if rates[i] == def {
			continue
		}
		if len(runs) > 0 && runs[len(runs)-1].end == i-1 && runs[len(runs)-1].rate == rates[i] {
			runs[len(runs)-1].end = i
			continue
		}
		runs = append(runs, bucketRun{start: i, end: i, rate: rates[i]})
	}
	return runs
}

// DistinctRates counts the different rate values in a bucket table — used to
// detect dynamic (Agile-like) pricing that cannot be expressed as bands.
func DistinctRates(rates *[BucketsPerDay]float64) int {
	seen := make(map[float64]struct{}, BucketsPerDay)
	for _, v := range rates {
		seen[v] = struct{}{}
	}
	return len(seen)
}

// MeanRate is the simple per-bucket average, the fallback default when band
// derivation is not possible.
func MeanRate(rates *[BucketsPerDay]float64) float64 {
	total := 0.0
	for _, v := range rates {
		total += v
	}
	return total / BucketsPerDay
}

// modalRate returns the most frequent value; ties break toward the lower
// rate so the result is deterministic and the cheaper rate is the default.
func modalRate(rates *[BucketsPerDay]float64) float64 {
	counts := make(map[float64]int, BucketsPerDay)
	for _, v := range rates {
		counts[v]++
	}
	best, bestCount := 0.0, -1
	for v, n := range counts {
		if n > bestCount || (n == bestCount && v < best) {
			best, bestCount = v, n
		}
	}
	return best
}

// bucketsPerHour converts a bucket index to its hour and half-hour offset.
const bucketsPerHour = 2

// bucketLabel renders bucket i as its "HH:MM" local start time.
func bucketLabel(i int) string {
	return fmt.Sprintf("%02d:%02d", i/bucketsPerHour, (i%bucketsPerHour)*minutesPerBucket)
}
