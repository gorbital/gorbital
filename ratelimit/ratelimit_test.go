package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gorbital.dev/ratelimit"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestAllowBurstAndRefill(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	l := ratelimit.New(1, 3, ratelimit.WithClock(c.Now))

	for i := range 3 {
		if ok, _ := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("Allow() call %d within burst = false, want true", i+1)
		}
	}
	ok, retry := l.Allow("1.2.3.4")
	if ok || retry <= 0 || retry > time.Second {
		t.Errorf("Allow() over burst = %t, retry %v; want false, (0,1s]", ok, retry)
	}
	if ok, _ := l.Allow("5.6.7.8"); !ok {
		t.Error("Allow(other key) = false, want independent bucket")
	}

	c.Advance(time.Second)
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Error("Allow() after 1s refill = false, want true")
	}
}

func TestMaxKeysFailsOpenAndEvictsIdle(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	l := ratelimit.New(0.001, 1, ratelimit.WithClock(c.Now), ratelimit.WithMaxKeys(2), ratelimit.WithIdleTTL(time.Minute))

	l.Allow("a")
	l.Allow("b")
	// Table full, nothing idle: new keys pass untracked.
	for range 3 {
		if ok, _ := l.Allow("c"); !ok {
			t.Fatal("Allow(untracked key with full table) = false, want fail open")
		}
	}
	// After the idle TTL, old keys are evicted and "c" becomes tracked.
	c.Advance(2 * time.Minute)
	if ok, _ := l.Allow("c"); !ok {
		t.Fatal("Allow(c) after eviction = false, want true (first token)")
	}
	if ok, _ := l.Allow("c"); ok {
		t.Error("Allow(c) second time = true, want limited once tracked")
	}
}

func TestMiddleware(t *testing.T) {
	l := ratelimit.New(0.001, 1)
	h := ratelimit.Middleware(l, ratelimit.ByRemoteIP, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	do := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/auth/login", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if got := do("10.0.0.1:5000").Code; got != http.StatusNoContent {
		t.Fatalf("first request = %d, want 204", got)
	}
	rec := do("10.0.0.1:5001") // same IP, different port
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("second request = %d, Retry-After %q, type %q; want 429 problem with Retry-After", rec.Code, rec.Header().Get("Retry-After"), rec.Header().Get("Content-Type"))
	}
	if got := do("10.0.0.2:5000").Code; got != http.StatusNoContent {
		t.Errorf("request from other IP = %d, want 204", got)
	}
}

// One IPv6 host holds a whole /64, so every address in it shares a budget
// (security review AUTH-S-7, OPS-1).
func TestByRemoteIPGroupsIPv6By64(t *testing.T) {
	l := ratelimit.New(0.001, 1)
	h := ratelimit.Middleware(l, ratelimit.ByRemoteIP, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	do := func(addr string) int {
		req := httptest.NewRequest("POST", "/v1/auth/login", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do("[2001:db8:1:2::1]:443"); got != http.StatusNoContent {
		t.Fatalf("first request = %d, want 204", got)
	}
	if got := do("[2001:db8:1:2:ffff:ffff:ffff:fffe]:443"); got != http.StatusTooManyRequests {
		t.Errorf("request from another address of the same /64 = %d, want 429", got)
	}
	if got := do("[2001:db8:1:3::1]:443"); got != http.StatusNoContent {
		t.Errorf("request from another /64 = %d, want 204", got)
	}
}

func TestClientKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":                    "203.0.113.9",
		"::ffff:203.0.113.9":             "203.0.113.9",
		"2001:db8:1:2:3:4:5:6":           "2001:db8:1:2::/64",
		"2001:DB8:1:2::9":                "2001:db8:1:2::/64",
		"fe80::1%eth0":                   "fe80::/64",
		"not-an-ip":                      "not-an-ip",
		"":                               "",
		"2001:db8:1:2:ffff:ffff:ffff:ff": "2001:db8:1:2::/64",
	} {
		if got := ratelimit.ClientKey(in); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", in, got, want)
		}
	}
}
