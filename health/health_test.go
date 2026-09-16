package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorbital.dev/health"
)

func serve(t *testing.T, h http.Handler) (int, health.Status, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	var s health.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return rec.Code, s, rec.Body.String()
}

func TestLiveness(t *testing.T) {
	c := health.New(nil, health.Check{Name: "db", Func: func(context.Context) error { return errors.New("down") }})
	if code, s, _ := serve(t, c.Liveness()); code != http.StatusOK || s.Status != "ok" {
		t.Errorf("Liveness() = %d %+v, want 200 ok even when dependencies fail", code, s)
	}
}

func TestReadiness(t *testing.T) {
	c := health.New(nil,
		health.Check{Name: "db", Func: func(context.Context) error { return nil }},
	)
	if code, s, _ := serve(t, c.Readiness()); code != http.StatusOK || s.Checks["db"].Status != "ok" {
		t.Errorf("Readiness() all passing = %d %+v, want 200 with db ok", code, s)
	}

	c.Add(health.Check{Name: "cache", Func: func(context.Context) error {
		return errors.New("dial tcp 10.0.3.7:6379: connection refused")
	}})
	code, s, body := serve(t, c.Readiness())
	if code != http.StatusServiceUnavailable || s.Status != "unavailable" || s.Checks["cache"].Status != "fail" {
		t.Errorf("Readiness() with failing check = %d %+v, want 503 with cache fail", code, s)
	}
	if strings.Contains(body, "10.0.3.7") {
		t.Errorf("Readiness() body leaks error details: %s", body)
	}

	c.SetShuttingDown()
	if code, s, _ := serve(t, c.Readiness()); code != http.StatusServiceUnavailable || s.Status != "shutting_down" {
		t.Errorf("Readiness() while shutting down = %d %+v, want 503 shutting_down", code, s)
	}
}

// TestReadinessSharesChecks checks that a flood of readiness requests runs
// the checks once, not once per request (HTTP-7).
func TestReadinessSharesChecks(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	c := health.New(nil, health.Check{Name: "db", Func: func(context.Context) error {
		runs.Add(1)
		<-release
		return nil
	}})
	h := c.Readiness()
	var wg sync.WaitGroup
	codes := make([]int, 50)
	for i := range codes {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
			codes[i] = rec.Code
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if code, _, _ := serve(t, h); code != http.StatusOK {
		t.Errorf("Readiness() right after = %d, want 200", code)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("51 readiness requests ran the checks %d times, want 1", n)
	}
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d = %d, want 200", i, code)
		}
	}
}

func TestReadinessTimeout(t *testing.T) {
	c := health.New(nil, health.Check{
		Name:    "slow",
		Timeout: 20 * time.Millisecond,
		Func: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	start := time.Now()
	status, ok := c.Check(context.Background())
	if ok || status.Checks["slow"].Status != "fail" {
		t.Errorf("Check() with slow dependency = %+v, %t; want fail", status, ok)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Check() took %v, want the 20ms timeout to apply", elapsed)
	}
}
