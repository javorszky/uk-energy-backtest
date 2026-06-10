package costing

import (
	"fmt"
	"time"
)

// Reading is one metered half-hour. IntervalStart is the absolute instant the
// period began (Octopus returns these with explicit offsets, so parsing them
// yields the correct instant regardless of representation).
type Reading struct {
	IntervalStart time.Time
	Consumption   float64
}

// localDateFormat keys the distinct-day set for supplied_days.
const localDateFormat = "2006-01-02"

// BuildProfile aggregates raw half-hourly readings into the 48-bucket load
// profile (costing rules 1, 3, 5, 6). This is the server-side twin of the
// frontend's buildProfile in frontend/src/lib/profile.ts — the shared fixture
// keeps the two in agreement.
//
// Each reading is bucketed by its *local* (loc, normally Europe/London)
// clock time, not UTC: bucket i covers local minute i*30. On DST-change days
// two UTC half-hours can land in the same local bucket (autumn) or a local
// hour can receive none (spring) — both are correct, because tariff bands are
// defined in local clock time.
//
// gasIsM3 selects the rule-5 m³→kWh conversion; cv is the calorific value in
// MJ/m³ (pass DefaultCalorificValue unless the user overrides). Conversion
// happens per reading, then each slot is rounded to 0.01 kWh half-to-even
// (rule 3), then summed.
func BuildProfile(imp, exp, gas []Reading, gasIsM3 bool, cv float64, loc *time.Location) (Profile, error) {
	if loc == nil {
		return Profile{}, fmt.Errorf("build profile: nil location")
	}

	p := Profile{}

	days := make(map[string]struct{})
	for _, r := range imp {
		local := r.IntervalStart.In(loc)
		p.ImportHH[bucketIndex(local)] += RoundHalfEven2dp(r.Consumption)
		days[local.Format(localDateFormat)] = struct{}{}
	}
	p.SuppliedDays = len(days)

	if len(exp) > 0 {
		var exportHH [BucketsPerDay]float64
		for _, r := range exp {
			exportHH[bucketIndex(r.IntervalStart.In(loc))] += RoundHalfEven2dp(r.Consumption)
		}
		p.ExportHH = &exportHH
	}

	for _, r := range gas {
		kwh := r.Consumption
		if gasIsM3 {
			kwh = ConvertM3ToKWh(kwh, cv)
		}
		p.GasKWh += RoundHalfEven2dp(kwh)
	}

	return p, nil
}

// bucketIndex maps a local time to its half-hour-of-day bucket (0..47).
func bucketIndex(local time.Time) int {
	return (local.Hour()*minutesPerHour + local.Minute()) / minutesPerBucket
}
