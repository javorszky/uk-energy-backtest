package server

import (
	"fmt"

	"github.com/labstack/echo/v5"
)

// errorEnvelope is the standard API error shape:
// { "error": { "code": "…", "message": "…" } }.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes returned by the cost endpoints.
const (
	codeInvalidRequest = "invalid_request"
	codeMissingKey     = "missing_octopus_key"
	codeUpstreamError  = "upstream_error"
)

// jsonError writes the standard error envelope with the given HTTP status.
func jsonError(c *echo.Context, status int, code, message string) error {
	if err := c.JSON(status, errorEnvelope{Error: errorBody{Code: code, Message: message}}); err != nil {
		return fmt.Errorf("write error response: %w", err)
	}
	return nil
}
