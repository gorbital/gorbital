package app

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/telemetry"
)

// newMetricsServer returns the listener serving Prometheus metrics at
// GET /metrics on METRICS_ADDR, or nil when METRICS_ADDR is empty
// (ADR-0063). It has the API server's timeouts but none of its middleware
// and no authentication: bind it to an interface only your Prometheus can
// reach, never a public one.
func newMetricsServer(cfg Config, tel *telemetry.Telemetry) *httpx.Server {
	if cfg.MetricsAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", tel.MetricsHandler())
	return httpx.NewServer(cfg.MetricsAddr, mux, httpx.WithErrorLogger(tel.Logger()))
}

// MetricsServer returns the metrics listener, or nil when METRICS_ADDR is
// empty. Run runs it; tests run it directly.
func (a *App) MetricsServer() *httpx.Server { return a.metrics }

// checkMetricsAddr validates METRICS_ADDR: empty, or a host:port whose port
// differs from APP_ADDR's, so metrics never share the API's listener, even
// on another interface.
func checkMetricsAddr(addr, apiAddr string) error {
	if addr == "" {
		return nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("METRICS_ADDR %q is not host:port: %w", addr, err)
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return fmt.Errorf("METRICS_ADDR %q must have a port number, such as 127.0.0.1:9464", addr)
	}
	if _, apiPort, err := net.SplitHostPort(apiAddr); err == nil && n != 0 && port == apiPort {
		return fmt.Errorf("METRICS_ADDR %q must use another port than APP_ADDR %q: metrics are served on their own listener, never with the API", addr, apiAddr)
	}
	return nil
}
