package ratelimit_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorbital.dev/ratelimit"
)

func TestPer(t *testing.T) {
	l := ratelimit.Per(10, 15*time.Minute)
	if l.Burst != 10 || l.PerSecond != 10.0/900 || !l.Valid() {
		t.Errorf("Per(10, 15m) = %+v", l)
	}
	if (ratelimit.Limit{PerSecond: 1}).Valid() || (ratelimit.Limit{Burst: 1}).Valid() {
		t.Error("a limit without a rate or burst is valid")
	}
}

func TestLimiterTake(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	l := ratelimit.New(1, 2, ratelimit.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	for i := range 2 {
		if d, err := l.Take(ctx, "k"); err != nil || !d.Allowed {
			t.Fatalf("Take() %d = %+v, %v; want allowed", i, d, err)
		}
	}
	if d, err := l.Take(ctx, "k"); err != nil || d.Allowed || d.RetryAfter != time.Second {
		t.Errorf("Take() over the burst = %+v, %v; want refused for 1s", d, err)
	}
	if _, err := l.Take(ctx, ""); !errors.Is(err, ratelimit.ErrEmptyKey) {
		t.Errorf("Take(empty key) error = %v", err)
	}
}

// failing is a Taker that can't decide.
type failing struct{}

func (failing) Take(context.Context, string) (ratelimit.Decision, error) {
	return ratelimit.Decision{}, errors.New("store unavailable")
}

func TestMiddlewareAllowsWhenTakerFails(t *testing.T) {
	h := ratelimit.Middleware(failing{}, ratelimit.ByRemoteIP, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status with a failing limiter = %d, want the request to proceed", rec.Code)
	}
}
