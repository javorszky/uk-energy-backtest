// Package costing implements the tariff costing engine: a pure package with
// no HTTP or Echo imports so it unit-tests cleanly. It operates on the
// 48-bucket load profile contract shared with the frontend (see brief.md and
// testdata/shared/profile_fixture.json).
package costing

import (
	"fmt"
	"strconv"
)

// BucketsPerDay is the number of half-hour buckets in a local day. A profile
// always carries exactly this many slots per stream regardless of DST: on
// clock-change days multiple (or zero) UTC half-hours map to a local bucket.
const BucketsPerDay = 48

// minutesPerBucket is the bucket width in minutes of local clock time.
const minutesPerBucket = 30

// Profile is the compact, date-stripped load profile both ingest paths
// converge on. Band matching depends only on local half-hour-of-day, so this
// is exact (to the data's native half-hour resolution), not lossy.
type Profile struct {
	ExportHH     *[BucketsPerDay]float64 `json:"export_hh,omitempty"`
	ImportHH     [BucketsPerDay]float64  `json:"import_hh"`
	SuppliedDays int                     `json:"supplied_days"`
	GasKWh       float64                 `json:"gas_kwh"`
}

// Band is a time-of-use rate window. From/To are "HH:MM" local clock times
// constrained to :00/:30 boundaries (half-hourly metering cannot cost
// sub-half-hour boundaries). A band with From > To wraps past midnight.
type Band struct {
	From string  `json:"from"`
	To   string  `json:"to"`
	Rate float64 `json:"rate"`
}

// Electricity holds the electricity side of a tariff. All rates are
// VAT-inclusive pence, exactly as a consumer quote shows them.
type Electricity struct {
	ImportBands    []Band  `json:"import_bands"`
	ExportBands    []Band  `json:"export_bands"`
	StandingCharge float64 `json:"standing_charge"`
	ImportDefault  float64 `json:"import_default"`
	ExportDefault  float64 `json:"export_default"`
}

// Gas holds the gas side of a tariff. Flat rate, no bands.
type Gas struct {
	StandingCharge float64 `json:"standing_charge"`
	Rate           float64 `json:"rate"`
}

// Tariff is one priced offer to compare. Gas is optional.
type Tariff struct {
	Gas         *Gas        `json:"gas,omitempty"`
	Name        string      `json:"name"`
	Electricity Electricity `json:"electricity"`
}

// Result is the costed outcome for one tariff. Money fields are pence. The
// per-bucket rate arrays are returned so the frontend can overlay rate bands
// on the daily load-profile chart without re-implementing band matching.
type Result struct {
	Name          string                 `json:"name"`
	ImportPence   float64                `json:"import_p"`
	ExportCreditP float64                `json:"export_credit_p"`
	GasPence      float64                `json:"gas_p"`
	StandingPence float64                `json:"standing_p"`
	NetPence      float64                `json:"net_p"`
	ImportRates   [BucketsPerDay]float64 `json:"import_rates"`
	ExportRates   [BucketsPerDay]float64 `json:"export_rates"`
}

// Cost prices a profile against one tariff (costing rules 2, 4, 6, 7).
// Export is a credit: it is subtracted once, in NetPence only — the
// ExportCreditP field itself is positive.
func Cost(p *Profile, t Tariff) (Result, error) {
	r := Result{Name: t.Name}

	imp, impRates, err := costStream(&p.ImportHH, t.Electricity.ImportBands, t.Electricity.ImportDefault)
	if err != nil {
		return Result{}, fmt.Errorf("tariff %q import bands: %w", t.Name, err)
	}
	r.ImportPence = imp
	r.ImportRates = impRates

	exportHH := &[BucketsPerDay]float64{}
	if p.ExportHH != nil {
		exportHH = p.ExportHH
	}
	exp, expRates, err := costStream(exportHH, t.Electricity.ExportBands, t.Electricity.ExportDefault)
	if err != nil {
		return Result{}, fmt.Errorf("tariff %q export bands: %w", t.Name, err)
	}
	r.ExportCreditP = exp
	r.ExportRates = expRates

	// Gas is flat (rule 4). A tariff without a gas block prices any gas in
	// the profile at zero rather than erroring, so an electricity-only quote
	// can still be compared against a dual-fuel dataset.
	gasStanding := 0.0
	if t.Gas != nil {
		r.GasPence = p.GasKWh * t.Gas.Rate
		// Gas standing applies only when the dataset actually has gas;
		// otherwise an electricity-only household comparing a dual-fuel
		// quote would be charged for a meter it does not have.
		if p.GasKWh > 0 {
			gasStanding = t.Gas.StandingCharge
		}
	}

	// Rule 6: supplied_days counts distinct local calendar dates with import
	// readings, so data gaps don't mis-charge standing.
	r.StandingPence = float64(p.SuppliedDays) * (t.Electricity.StandingCharge + gasStanding)

	// Rule 7: net = import + gas + standing − export credit.
	r.NetPence = r.ImportPence + r.GasPence + r.StandingPence - r.ExportCreditP

	return r, nil
}

// costStream prices one 48-bucket stream against its bands (rules 2 and 4)
// and returns the total pence plus the per-bucket rate used.
func costStream(hh *[BucketsPerDay]float64, bands []Band, def float64) (total float64, rates [BucketsPerDay]float64, err error) {
	parsed, err := parseBands(bands)
	if err != nil {
		return 0, [BucketsPerDay]float64{}, err
	}

	for i := range BucketsPerDay {
		rate := rateFor(i*minutesPerBucket, parsed, def)
		rates[i] = rate
		total += hh[i] * rate
	}
	return total, rates, nil
}

// minuteBand is a Band with its window resolved to minutes of local day.
type minuteBand struct {
	from, to int
	rate     float64
}

func parseBands(bands []Band) ([]minuteBand, error) {
	out := make([]minuteBand, 0, len(bands))
	for _, b := range bands {
		from, err := parseHHMM(b.From)
		if err != nil {
			return nil, fmt.Errorf("band %q–%q: %w", b.From, b.To, err)
		}
		to, err := parseHHMM(b.To)
		if err != nil {
			return nil, fmt.Errorf("band %q–%q: %w", b.From, b.To, err)
		}
		out = append(out, minuteBand{from: from, to: to, rate: b.Rate})
	}
	return out, nil
}

// rateFor returns the rate for local minute-of-day t: the first band whose
// window contains t wins, else the default (rule 2). Window semantics:
// from <= t < to; a band with from > to wraps midnight and covers
// t >= from || t < to. A band with from == to is zero-width and never matches.
func rateFor(t int, bands []minuteBand, def float64) float64 {
	for _, b := range bands {
		if b.from > b.to {
			if t >= b.from || t < b.to {
				return b.rate
			}
			continue
		}
		if t >= b.from && t < b.to {
			return b.rate
		}
	}
	return def
}

const (
	minutesPerHour = 60
	hoursPerDay    = 24
	hhmmLen        = len("HH:MM")
)

// parseHHMM converts an "HH:MM" string to minute-of-day. Minutes must be 00
// or 30: half-hourly metering cannot cost sub-half-hour band boundaries, so
// anything else is rejected rather than silently mis-priced.
func parseHHMM(s string) (int, error) {
	if len(s) != hhmmLen || s[2] != ':' {
		return 0, fmt.Errorf("invalid time %q: want HH:MM", s)
	}
	h, err := strconv.Atoi(s[:2])
	if err != nil {
		return 0, fmt.Errorf("invalid hour in %q: %w", s, err)
	}
	m, err := strconv.Atoi(s[3:])
	if err != nil {
		return 0, fmt.Errorf("invalid minute in %q: %w", s, err)
	}
	if h < 0 || h >= hoursPerDay {
		return 0, fmt.Errorf("invalid time %q: hour out of range", s)
	}
	if m != 0 && m != minutesPerBucket {
		return 0, fmt.Errorf("invalid time %q: band boundaries must fall on :00 or :30", s)
	}
	return h*minutesPerHour + m, nil
}
