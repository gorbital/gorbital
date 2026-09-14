// Package health serves liveness and readiness endpoints.
//
// Liveness (/livez) only reports that the process is serving requests; it
// never checks dependencies, so a database outage doesn't restart every
// instance. Readiness (/readyz) runs registered checks and fails while the
// application is shutting down, so load balancers stop routing to it
// (ADR-0017).
//
// Stability: pre-1.0 (ADR-0015).
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultTimeout applies to checks with no timeout.
const DefaultTimeout = 2 * time.Second

// A Check verifies one dependency. Func must respect ctx.
type Check struct {
	Name    string
	Timeout time.Duration
	Func    func(ctx context.Context) error
}

// Checker holds readiness checks and shutdown state. It is safe for
// concurrent use.
type Checker struct {
	logger       *slog.Logger
	shuttingDown atomic.Bool

	mu     sync.RWMutex
	checks []Check
}

// New returns a Checker. A nil logger discards check failures.
func New(logger *slog.Logger, checks ...Check) *Checker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Checker{logger: logger, checks: checks}
}

// Add registers a readiness check.
func (c *Checker) Add(check Check) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks = append(c.checks, check)
}

// SetShuttingDown makes readiness fail from now on. Pass it to
// app.OnShutdown.
func (c *Checker) SetShuttingDown() { c.shuttingDown.Store(true) }

// Status is the readiness response body.
type Status struct {
	Status string                 `json:"status"`
	Checks map[string]CheckStatus `json:"checks,omitempty"`
}

// CheckStatus is one check's result. Error details are logged, not returned,
// so dependency hostnames and messages don't leak.
type CheckStatus struct {
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
}

// Liveness serves 200 {"status":"ok"} while the process can handle requests.
func (c *Checker) Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, Status{Status: "ok"})
	})
}

// Readiness runs every check concurrently and serves 200 when all pass, 503
// otherwise, or 503 {"status":"shutting_down"} after [Checker.SetShuttingDown].
func (c *Checker) Readiness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.shuttingDown.Load() {
			writeJSON(w, http.StatusServiceUnavailable, Status{Status: "shutting_down"})
			return
		}
		status, ok := c.Check(r.Context())
		code := http.StatusOK
		if !ok {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, status)
	})
}

// Check runs every check and reports whether all passed.
func (c *Checker) Check(ctx context.Context) (Status, bool) {
	c.mu.RLock()
	checks := append([]Check(nil), c.checks...)
	c.mu.RUnlock()

	results := make([]CheckStatus, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.run(ctx, check)
		}()
	}
	wg.Wait()

	status := Status{Status: "ok", Checks: make(map[string]CheckStatus, len(checks))}
	ok := true
	for i, check := range checks {
		status.Checks[check.Name] = results[i]
		if results[i].Status != "ok" {
			ok = false
			status.Status = "unavailable"
		}
	}
	return status, ok
}

func (c *Checker) run(ctx context.Context, check Check) CheckStatus {
	timeout := check.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	errc := make(chan error, 1)
	go func() { errc <- check.Func(ctx) }()

	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		err = ctx.Err()
	}
	res := CheckStatus{Status: "ok", DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Status = "fail"
		c.logger.WarnContext(ctx, "readiness check failed", "check", check.Name, "err", err)
	}
	return res
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
