package server

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/javorszky/uk-energy-backtest/internal/costing"
)

// maxTariffsPerRequest bounds the work a single request can ask for. Costing
// is cheap, but an unbounded list is an easy abuse vector on a public,
// unauthenticated endpoint.
const maxTariffsPerRequest = 50

// profilePayload is the wire shape of a profile. It binds the bucket arrays
// as slices rather than [48]float64 because encoding/json silently discards
// elements beyond a fixed array's length — a 49-bucket payload would
// otherwise be truncated instead of rejected.
type profilePayload struct {
	ImportHH     []float64 `json:"import_hh"`
	ExportHH     []float64 `json:"export_hh"`
	SuppliedDays int       `json:"supplied_days"`
	GasKWh       float64   `json:"gas_kwh"`
}

// toProfile validates the payload and converts it to the engine type.
// Returns a non-empty message on validation failure.
func (pp profilePayload) toProfile() (p costing.Profile, msg string) {
	if len(pp.ImportHH) != costing.BucketsPerDay {
		return costing.Profile{}, fmt.Sprintf("import_hh must contain exactly %d buckets", costing.BucketsPerDay)
	}
	if pp.ExportHH != nil && len(pp.ExportHH) != costing.BucketsPerDay {
		return costing.Profile{}, fmt.Sprintf("export_hh must contain exactly %d buckets", costing.BucketsPerDay)
	}

	p = costing.Profile{SuppliedDays: pp.SuppliedDays, GasKWh: pp.GasKWh}
	copy(p.ImportHH[:], pp.ImportHH)
	if pp.ExportHH != nil {
		var exportHH [costing.BucketsPerDay]float64
		copy(exportHH[:], pp.ExportHH)
		p.ExportHH = &exportHH
	}
	if msg = validateProfile(&p); msg != "" {
		return costing.Profile{}, msg
	}
	return p, ""
}

type costRequest struct {
	Tariffs []costing.Tariff `json:"tariffs"`
	Profile profilePayload   `json:"profile"`
}

type costResponse struct {
	Results []costing.Result `json:"results"`
}

// costHandler implements POST /api/v1/cost: fully stateless, takes a load
// profile plus tariffs and returns a costed result per tariff. Nothing is
// retained after the response is written.
func costHandler(c *echo.Context) error {
	var req costRequest
	if err := c.Bind(&req); err != nil {
		return jsonError(c, http.StatusBadRequest, codeInvalidRequest, "malformed request body")
	}

	if msg := validateTariffCount(req.Tariffs); msg != "" {
		return jsonError(c, http.StatusBadRequest, codeInvalidRequest, msg)
	}
	profile, msg := req.Profile.toProfile()
	if msg != "" {
		return jsonError(c, http.StatusBadRequest, codeInvalidRequest, msg)
	}

	results, err := costAll(&profile, req.Tariffs)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, codeInvalidRequest, err.Error())
	}

	if err := c.JSON(http.StatusOK, costResponse{Results: results}); err != nil {
		return fmt.Errorf("write cost response: %w", err)
	}
	return nil
}

func validateTariffCount(tariffs []costing.Tariff) string {
	if len(tariffs) == 0 {
		return "at least one tariff is required"
	}
	if len(tariffs) > maxTariffsPerRequest {
		return fmt.Sprintf("at most %d tariffs per request", maxTariffsPerRequest)
	}
	return ""
}

func validateProfile(p *costing.Profile) string {
	if p.SuppliedDays < 0 {
		return "supplied_days must not be negative"
	}
	if p.GasKWh < 0 {
		return "gas_kwh must not be negative"
	}
	for i, v := range p.ImportHH {
		if v < 0 {
			return fmt.Sprintf("import_hh[%d] must not be negative", i)
		}
	}
	if p.ExportHH != nil {
		for i, v := range p.ExportHH {
			if v < 0 {
				return fmt.Sprintf("export_hh[%d] must not be negative", i)
			}
		}
	}
	return ""
}

func costAll(p *costing.Profile, tariffs []costing.Tariff) ([]costing.Result, error) {
	results := make([]costing.Result, 0, len(tariffs))
	for _, t := range tariffs {
		r, err := costing.Cost(p, t)
		if err != nil {
			return nil, fmt.Errorf("cost tariff: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}
