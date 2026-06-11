// Package server configures and runs the Echo HTTP server.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/javorszky/uk-energy-backtest/internal/config"
	"github.com/javorszky/uk-energy-backtest/internal/octopus"
)

const (
	bodyLimitBytes    = 10 * 1024 * 1024 // 10 MiB
	gracefulTimeout   = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	// octopusRequestTimeout is the per-HTTP-request (per page) timeout for
	// upstream Octopus calls; the handler separately bounds the whole
	// pipeline at octopusTimeout.
	octopusRequestTimeout = 30 * time.Second
)

// Server wraps the Echo instance and the address it will listen on.
type Server struct {
	echo *echo.Echo
	addr string
}

// New creates and configures a Server.
func New(cfg config.Config, gitSHA, buildTime string) *Server {
	e := echo.New()

	e.Use(middleware.Recover())
	e.Use(otelMiddleware(cfg.ServiceName))
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:   true,
		LogURI:      true,
		LogStatus:   true,
		LogLatency:  true,
		HandleError: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error == nil {
				slog.LogAttrs((*c).Request().Context(), slog.LevelInfo, "request",
					slog.String("method", v.Method),
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Duration("latency", v.Latency),
				)
			} else {
				slog.LogAttrs((*c).Request().Context(), slog.LevelError, "request",
					slog.String("method", v.Method),
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Duration("latency", v.Latency),
					slog.String("error", v.Error.Error()),
				)
			}
			return nil
		},
	}))

	e.Use(middleware.BodyLimit(bodyLimitBytes))

	// CORS is only needed in decoupled deployments where the frontend and
	// backend run on different origins. In embedded mode they share an origin
	// so no CORS headers are required.
	if cfg.FrontendOrigin != "" {
		e.Use(middleware.CORS(cfg.FrontendOrigin))
	}

	// Europe/London is guaranteed present by the time/tzdata blank import in
	// cmd/server; failure here means a broken build, so fail fast.
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(fmt.Sprintf("load Europe/London tzdata: %v", err))
	}

	v1 := e.Group("/api/v1")
	v1.GET("/health", healthHandler)
	v1.GET("/status", statusHandler(gitSHA, buildTime))
	v1.POST("/cost", costHandler)
	octoClient := octopus.NewClient(octopusRequestTimeout)
	v1.POST("/octopus/cost", octopusCostHandler(octoClient, london))
	v1.POST("/octopus/tariff", octopusTariffHandler(octoClient, london))

	// The Octopus OAuth connect flow is gated on a client id being
	// configured; without one the config endpoint reports disabled and the
	// token exchange endpoint does not exist.
	v1.GET("/oauth/config", oauthConfigHandler(cfg.OctopusOAuthClientID))
	if cfg.OctopusOAuthClientID != "" {
		v1.POST("/oauth/token", oauthTokenHandler(octopus.NewOAuthClient(octopusRequestTimeout), cfg.OctopusOAuthClientID))
	}

	registerStatic(e)

	return &Server{echo: e, addr: fmt.Sprintf(":%d", cfg.Port)}
}

// Start runs the server until ctx is cancelled, then shuts down gracefully.
func (s *Server) Start(ctx context.Context) error {
	sc := echo.StartConfig{
		Address:         s.addr,
		GracefulTimeout: gracefulTimeout,
		BeforeServeFunc: func(srv *http.Server) error {
			srv.ReadHeaderTimeout = readHeaderTimeout
			srv.ReadTimeout = readTimeout
			return nil
		},
	}
	if err := sc.Start(ctx, s.echo); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	return nil
}

// Handler returns the underlying http.Handler, useful for testing routes
// without starting a real listener.
func (s *Server) Handler() http.Handler {
	return s.echo
}

// healthHandler is a liveness probe: it returns 200 as long as the process
// responds. It does not check dependencies. When the first backing service
// lands, split into /livez (always 200) and /readyz (checks deps) and
// deprecate this endpoint.
func healthHandler(c *echo.Context) error {
	if err := c.JSON(http.StatusOK, map[string]string{"status": "ok"}); err != nil {
		return fmt.Errorf("write response: %w", err)
	}

	return nil
}
