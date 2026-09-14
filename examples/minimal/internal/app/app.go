// Package app is the composition root of acme-api. It is the only package
// that reads configuration, and it builds, wires, runs and shuts down every
// component in a defined order.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	lifecycle "apistock.dev/app"
	"apistock.dev/buildinfo"
	"apistock.dev/health"
	"apistock.dev/httpx"
	"apistock.dev/modules/openapi"
	"apistock.dev/modules/telemetry"
)

// ServiceName identifies the service in logs, traces and docs.
const ServiceName = "acme-api"

// App is the wired application.
type App struct {
	cfg     Config
	logger  *slog.Logger
	tel     *telemetry.Telemetry
	health  *health.Checker
	cleanup *lifecycle.Cleanup
	api     huma.API
	handler http.Handler
}

// New builds the application. Components are constructed in dependency
// order; each registers its resources on the cleanup stack.
func New(ctx context.Context, cfg Config) (*App, error) {
	cleanup := &lifecycle.Cleanup{}

	format := telemetry.LogFormatText
	if cfg.Production() {
		format = telemetry.LogFormatJSON
	}
	tel, err := telemetry.Setup(ctx, ServiceName, buildinfo.Read().Version,
		telemetry.WithOTLPExport(cfg.OTLPEndpoint != ""),
		telemetry.WithLogFormat(format),
		telemetry.WithLogLevel(cfg.LogLevel),
	)
	if err != nil {
		return nil, err
	}
	cleanup.Add("telemetry", tel.Shutdown) // registered first, closed last

	a := &App{
		cfg:     cfg,
		logger:  tel.Logger(),
		tel:     tel,
		health:  health.New(tel.Logger()),
		cleanup: cleanup,
	}
	if err := a.buildHTTP(); err != nil {
		return nil, errors.Join(err, cleanup.Close(ctx))
	}
	return a, nil
}

// Run serves HTTP until a shutdown signal, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	server := httpx.NewServer(a.cfg.Addr, a.handler, httpx.WithErrorLogger(a.logger))

	opts := []lifecycle.Option{
		lifecycle.WithCleanup(a.cleanup),
		lifecycle.WithLogger(a.logger),
		lifecycle.OnShutdown(a.health.SetShuttingDown),
	}
	if !a.cfg.Production() {
		opts = append(opts, lifecycle.WithDrainDelay(0)) // no load balancer to drain locally
	}

	a.logger.InfoContext(ctx, "starting", "addr", "http://"+a.cfg.Addr, "docs_enabled", a.cfg.DocsEnabled)
	return lifecycle.Run(ctx, []lifecycle.Runner{server}, opts...)
}

// Handler returns the HTTP handler, for tests.
func (a *App) Handler() http.Handler { return a.handler }

// Close releases resources without running the server, for tests and
// one-off commands.
func (a *App) Close(ctx context.Context) error { return a.cleanup.Close(ctx) }

// WriteOpenAPI writes the API's OpenAPI document to w.
func WriteOpenAPI(ctx context.Context, cfg Config, w io.Writer) (err error) {
	a, err := New(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close(ctx)) }()
	if err := openapi.WriteSpec(w, a.api); err != nil {
		return fmt.Errorf("export openapi: %w", err)
	}
	return nil
}
