package idempotency_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/modules/idempotency"
	"gorbital.dev/modules/postgres/pgtest"
)

func newStore(t testing.TB, opts ...idempotency.Option) (*idempotency.Store, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.New(t, pgtest.WithMigrations(idempotency.Migrations))
	s, err := idempotency.NewStore(pool, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s, pool
}

// counting is a handler that counts its runs and creates a numbered
// resource.
type counting struct {
	runs    atomic.Int32
	respond func(w http.ResponseWriter, r *http.Request, run int32)
}

func (c *counting) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := c.runs.Add(1)
	if c.respond != nil {
		c.respond(w, r, n)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/v1/things/%d", n))
	w.Header().Set("ETag", fmt.Sprintf(`"v%d"`, n))
	w.Header().Set("X-Not-Replayed", "yes")
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, `{"id":%d}`, n)
}

// serve wraps next in the middleware, behind a stand-in for authentication
// that signs requests in as the X-Test-Actor header ("user:usr_1").
func serve(s *idempotency.Store, next http.Handler, opts ...idempotency.MiddlewareOption) http.Handler {
	h := idempotency.Middleware(s, opts...)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if kind, id, ok := strings.Cut(r.Header.Get("X-Test-Actor"), ":"); ok {
			r = r.WithContext(actor.With(r.Context(), actor.Actor{Kind: actor.Kind(kind), ID: id}))
		}
		h.ServeHTTP(w, r)
	})
}

// send makes a request as caller ("" for anonymous) with key ("" for none).
func send(h http.Handler, method, target, body, caller, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if caller != "" {
		req.Header.Set("X-Test-Actor", caller)
	}
	if key != "" {
		req.Header.Set(idempotency.Header, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func problemCode(rec *httptest.ResponseRecorder) string {
	var p struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

// TestReplayFidelity checks that a retry gets the first response's status,
// replayed headers and body, marked as replayed, without running again.
func TestReplayFidelity(t *testing.T) {
	s, _ := newStore(t)
	next := &counting{}
	h := serve(s, next)

	first := send(h, "POST", "/v1/things?dry=false", `{"name":"a"}`, "user:usr_1", "key-1")
	if first.Code != http.StatusCreated || first.Header().Get(idempotency.ReplayedHeader) != "" {
		t.Fatalf("first request = %d %v", first.Code, first.Header())
	}
	for range 2 {
		again := send(h, "POST", "/v1/things?dry=false", `{"name":"a"}`, "user:usr_1", "key-1")
		if again.Code != http.StatusCreated || again.Body.String() != first.Body.String() {
			t.Errorf("replay = %d %s, want %d %s", again.Code, again.Body, first.Code, first.Body)
		}
		for _, name := range []string{"Content-Type", "Location", "ETag"} {
			if again.Header().Get(name) != first.Header().Get(name) {
				t.Errorf("replayed %s = %q, want %q", name, again.Header().Get(name), first.Header().Get(name))
			}
		}
		if again.Header().Get(idempotency.ReplayedHeader) != "true" || again.Header().Get("X-Not-Replayed") != "" {
			t.Errorf("replay headers = %v, want Idempotent-Replayed and only the stored headers", again.Header())
		}
	}
	if next.runs.Load() != 1 {
		t.Errorf("handler ran %d times, want 1", next.runs.Load())
	}

	// PATCH is covered too; stored client errors are replayed like successes.
	next.respond = func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"name_taken"}`))
	}
	for range 2 {
		if r := send(h, "PATCH", "/v1/things/1", `{"name":"b"}`, "user:usr_1", "key-2"); r.Code != http.StatusConflict || r.Body.String() != `{"code":"name_taken"}` {
			t.Errorf("PATCH = %d %s, want the stored 409", r.Code, r.Body)
		}
	}
	// GET, PUT and DELETE ignore the header.
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		send(h, method, "/v1/things/1", "", "user:usr_1", "key-3")
		send(h, method, "/v1/things/1", "", "user:usr_1", "key-3")
	}
	if want := int32(1 + 1 + 6); next.runs.Load() != want {
		t.Errorf("handler ran %d times, want %d", next.runs.Load(), want)
	}
}

// TestConcurrentRequests sends a second request while the first one with the
// same key is still running: it gets 409, and a retry after the first
// finishes gets its response.
func TestConcurrentRequests(t *testing.T) {
	s, _ := newStore(t)
	started, release := make(chan struct{}), make(chan struct{})
	next := &counting{}
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next.runs.Load() == 0 {
			close(started)
			<-release
		}
		next.ServeHTTP(w, r)
	})
	h := serve(s, slow)

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- send(h, "POST", "/v1/things", `{}`, "user:usr_1", "race") }()
	<-started
	second := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "race")
	if second.Code != http.StatusConflict || problemCode(second) != "idempotency_in_progress" || second.Header().Get("Retry-After") == "" {
		t.Errorf("request during the first = %d %s %v, want 409 idempotency_in_progress with Retry-After", second.Code, second.Body, second.Header())
	}
	close(release)
	if first := <-done; first.Code != http.StatusCreated {
		t.Fatalf("first request = %d %s", first.Code, first.Body)
	}
	if again := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "race"); again.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Errorf("request after the first = %d %v, want the replay", again.Code, again.Header())
	}
	if next.runs.Load() != 1 {
		t.Errorf("handler ran %d times, want 1", next.runs.Load())
	}
}

// TestRacingRequestsRunOnce sends many identical requests at once, as
// several instances would receive them: the handler runs once, and every
// other request is refused as in progress or replays the response.
func TestRacingRequestsRunOnce(t *testing.T) {
	s, _ := newStore(t)
	next := &counting{}
	slowed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		next.ServeHTTP(w, r)
	})
	h := serve(s, slowed)
	var created, conflicts, replays atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			r := send(h, "POST", "/v1/things", `{"n":1}`, "user:usr_1", "same-key")
			switch {
			case r.Code == http.StatusCreated && r.Header().Get(idempotency.ReplayedHeader) == "true":
				replays.Add(1)
			case r.Code == http.StatusCreated:
				created.Add(1)
			case r.Code == http.StatusConflict && problemCode(r) == "idempotency_in_progress":
				conflicts.Add(1)
			default:
				t.Errorf("racing request = %d %s", r.Code, r.Body)
			}
		})
	}
	wg.Wait()
	if next.runs.Load() != 1 || created.Load() != 1 || conflicts.Load()+replays.Load() != 19 {
		t.Errorf("runs %d, created %d, in progress %d, replayed %d; want the handler to run once",
			next.runs.Load(), created.Load(), conflicts.Load(), replays.Load())
	}
}

// TestFingerprintMismatch reuses a key for a different request.
func TestFingerprintMismatch(t *testing.T) {
	s, _ := newStore(t)
	next := &counting{}
	h := serve(s, next)
	if r := send(h, "POST", "/v1/things?a=1", `{"name":"a"}`, "user:usr_1", "k"); r.Code != http.StatusCreated {
		t.Fatalf("first = %d", r.Code)
	}
	for _, tt := range []struct{ name, method, target, body string }{
		{"another body", "POST", "/v1/things?a=1", `{"name":"b"}`},
		{"another path", "POST", "/v1/others?a=1", `{"name":"a"}`},
		{"another query", "POST", "/v1/things?a=2", `{"name":"a"}`},
		{"another method", "PATCH", "/v1/things?a=1", `{"name":"a"}`},
	} {
		r := send(h, tt.method, tt.target, tt.body, "user:usr_1", "k")
		if r.Code != http.StatusUnprocessableEntity || problemCode(r) != "idempotency_key_reused" {
			t.Errorf("%s = %d %s, want 422 idempotency_key_reused", tt.name, r.Code, r.Body)
		}
	}
	if next.runs.Load() != 1 {
		t.Errorf("handler ran %d times, want 1", next.runs.Load())
	}
}

// TestCallersDontShareKeys sends the same key and body as different callers:
// each runs its own request and replays only its own response. Anonymous
// requests ignore the header.
func TestCallersDontShareKeys(t *testing.T) {
	s, pool := newStore(t)
	next := &counting{}
	h := serve(s, next)
	bodies := map[string]string{}
	for _, caller := range []string{"user:usr_1", "user:usr_2", "service:usr_1"} {
		r := send(h, "POST", "/v1/things", `{}`, caller, "shared")
		if r.Code != http.StatusCreated || r.Header().Get(idempotency.ReplayedHeader) != "" {
			t.Fatalf("%s's first request = %d %v, want its own run", caller, r.Code, r.Header())
		}
		bodies[caller] = r.Body.String()
	}
	for caller, body := range bodies {
		if r := send(h, "POST", "/v1/things", `{}`, caller, "shared"); r.Body.String() != body {
			t.Errorf("%s's replay = %s, want its own %s", caller, r.Body, body)
		}
	}
	for range 2 {
		if r := send(h, "POST", "/v1/things", `{}`, "", "shared"); r.Header().Get(idempotency.ReplayedHeader) != "" {
			t.Error("an anonymous request was replayed")
		}
		if r := send(h, "POST", "/v1/things", `{}`, "system:jobs", "shared"); r.Header().Get(idempotency.ReplayedHeader) != "" {
			t.Error("a system actor's request was replayed")
		}
	}
	if next.runs.Load() != 3+4 {
		t.Errorf("handler ran %d times, want 7", next.runs.Load())
	}

	// Neither callers' IDs nor keys are stored.
	var raw int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_keys
		WHERE position(convert_to('shared', 'UTF8') in id) > 0 OR position(convert_to('usr_1', 'UTF8') in id) > 0`).Scan(&raw)
	if err != nil || raw != 0 {
		t.Errorf("rows containing a raw key or user ID = %d, %v", raw, err)
	}
}

// TestReleasedResponses checks that responses that aren't final outcomes
// release the key, so a retry runs again: server errors, panics, statuses a
// caller resolves without changing the request, cookies, bodies over the cap,
// and DontStore.
func TestReleasedResponses(t *testing.T) {
	s, pool := newStore(t)
	for _, tt := range []struct {
		name    string
		respond func(w http.ResponseWriter, r *http.Request)
	}{
		{"500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }},
		{"503", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }},
		{"panic", func(http.ResponseWriter, *http.Request) { panic("boom") }},
		{"401", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }},
		{"403", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }},
		{"429", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }},
		{"Set-Cookie", func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "secret"})
			_, _ = w.Write([]byte(`{"token":"secret"}`))
		}},
		{"body over the cap", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", 60)))
			_, _ = w.Write([]byte(strings.Repeat("y", 60)))
		}},
		{"DontStore", func(w http.ResponseWriter, r *http.Request) {
			idempotency.DontStore(r.Context())
			_, _ = w.Write([]byte(`{"api_key":"shown once"}`))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var runs atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				runs.Add(1)
				tt.respond(w, r)
			})
			// Stands in for httpx.Recover, outside the middleware.
			recovered := serve(s, handler, idempotency.WithMaxResponseBytes(100))
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if recover() != nil {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				recovered.ServeHTTP(w, r)
			})
			key := "released-" + strings.ReplaceAll(tt.name, " ", "-")
			first := send(h, "POST", "/v1/things", `{}`, "user:usr_1", key)
			second := send(h, "POST", "/v1/things", `{}`, "user:usr_1", key)
			if runs.Load() != 2 || second.Header().Get(idempotency.ReplayedHeader) != "" {
				t.Errorf("handler ran %d times (responses %d, %d), want the retry to run again", runs.Load(), first.Code, second.Code)
			}
			if tt.name == "body over the cap" && first.Body.Len() != 120 {
				t.Errorf("client got %d bytes, want the whole body", first.Body.Len())
			}
		})
	}
	var rows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_keys`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("stored keys = %d, %v; want none", rows, err)
	}

	// A body exactly at the cap is stored.
	next := &counting{respond: func(w http.ResponseWriter, _ *http.Request, _ int32) {
		_, _ = w.Write([]byte(strings.Repeat("z", 100)))
	}}
	h := serve(s, next, idempotency.WithMaxResponseBytes(100))
	send(h, "POST", "/v1/things", `{}`, "user:usr_1", "at-cap")
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "at-cap"); r.Header().Get(idempotency.ReplayedHeader) != "true" || r.Body.Len() != 100 || next.runs.Load() != 1 {
		t.Errorf("replay of a body at the cap = %v, %d bytes, %d runs", r.Header(), r.Body.Len(), next.runs.Load())
	}
}

// TestSkippedRequests checks WithSkip, for sign-in routes.
func TestSkippedRequests(t *testing.T) {
	s, _ := newStore(t)
	next := &counting{}
	h := serve(s, next, idempotency.WithSkip(func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/v1/auth/") }))
	for range 2 {
		if r := send(h, "POST", "/v1/auth/login", `{}`, "user:usr_1", "login"); r.Header().Get(idempotency.ReplayedHeader) != "" {
			t.Error("a skipped route was replayed")
		}
	}
	if next.runs.Load() != 2 {
		t.Errorf("handler ran %d times, want 2", next.runs.Load())
	}
}

func TestInvalidKeys(t *testing.T) {
	s, _ := newStore(t)
	next := &counting{}
	h := serve(s, next)
	for name, key := range map[string]string{
		"too long":      strings.Repeat("k", idempotency.MaxKeyLength+1),
		"space":         "two words",
		"non-ASCII":     "clé",
		"control":       "key\t1",
		"only a space?": " ",
	} {
		if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", key); r.Code != http.StatusBadRequest || problemCode(r) != "invalid_idempotency_key" {
			t.Errorf("%s key = %d %s, want 400 invalid_idempotency_key", name, r.Code, r.Body)
		}
	}
	req := httptest.NewRequest("POST", "/v1/things", strings.NewReader(`{}`))
	req.Header.Set("X-Test-Actor", "user:usr_1")
	req.Header.Add(idempotency.Header, "a")
	req.Header.Add(idempotency.Header, "b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("two keys = %d, want 400", rec.Code)
	}
	if next.runs.Load() != 0 {
		t.Errorf("handler ran %d times for invalid keys", next.runs.Load())
	}
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", strings.Repeat("~", idempotency.MaxKeyLength)); r.Code != http.StatusCreated {
		t.Errorf("255-character key = %d %s, want 201", r.Code, r.Body)
	}
}

// TestRequestBodyLimit checks that a body over the server's limit reaches the
// handler's own error handling rather than being stored or fingerprinted.
func TestRequestBodyLimit(t *testing.T) {
	s, _ := newStore(t)
	var runs atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs.Add(1)
		if _, err := io.ReadAll(r.Body); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	inner := serve(s, handler)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10)
		inner.ServeHTTP(w, r)
	})
	for range 2 {
		if r := send(h, "POST", "/v1/things", strings.Repeat("x", 11), "user:usr_1", "big"); r.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("body over the limit = %d, want the handler's 413", r.Code)
		}
	}
	if runs.Load() != 2 {
		t.Errorf("handler ran %d times, want 2", runs.Load())
	}
}

// TestStaleLockExpires simulates an instance that crashed holding a key: the
// key answers 409 until the lock TTL passes, then the same request runs; a
// different request is still refused.
func TestStaleLockExpires(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	s, _ := newStore(t, idempotency.WithClock(func() time.Time { return now }), idempotency.WithLockTTL(time.Minute))
	ctx := context.Background()
	fp := idempotency.Fingerprint("POST", "/v1/things", []byte(`{}`))
	crashed, _, err := s.Claim(ctx, "user:usr_1", "crash", fp)
	if err != nil || crashed == nil {
		t.Fatalf("Claim() = %v, %v", crashed, err)
	}

	next := &counting{}
	h := serve(s, next)
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "crash"); r.Code != http.StatusConflict {
		t.Errorf("request while the lock is held = %d, want 409", r.Code)
	}
	now = now.Add(time.Minute)
	if r := send(h, "POST", "/v1/things", `{"other":true}`, "user:usr_1", "crash"); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("different request after the lock expired = %d, want 422", r.Code)
	}
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "crash"); r.Code != http.StatusCreated || next.runs.Load() != 1 {
		t.Errorf("same request after the lock expired = %d, %d runs; want it to run", r.Code, next.runs.Load())
	}

	// The crashed request can't overwrite or delete the key it lost.
	if err := crashed.Complete(ctx, idempotency.Response{Status: 500}); !errors.Is(err, idempotency.ErrLockLost) {
		t.Errorf("Complete() on a lost lock = %v, want ErrLockLost", err)
	}
	if err := crashed.Release(ctx); err != nil {
		t.Errorf("Release() on a lost lock = %v", err)
	}
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "crash"); r.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Errorf("request after the lost lock's calls = %d %v, want the replay", r.Code, r.Header())
	}
}

// TestRetentionAndCleanup checks that keys expire after the retention read
// on every use, and that DeleteExpired removes them.
func TestRetentionAndCleanup(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	retention := 24 * time.Hour
	s, _ := newStore(t,
		idempotency.WithClock(func() time.Time { return now }),
		idempotency.WithRetention(func(context.Context) time.Duration { return retention }))
	ctx := context.Background()
	next := &counting{}
	h := serve(s, next)

	send(h, "POST", "/v1/things", `{"a":1}`, "user:usr_1", "old")
	if oldest, ok, err := s.Oldest(ctx); err != nil || !ok || !oldest.Equal(now) {
		t.Errorf("Oldest() = %v, %v, %v; want %v", oldest, ok, err, now)
	}
	now = now.Add(2 * time.Hour)
	send(h, "POST", "/v1/things", `{"a":1}`, "user:usr_1", "new")

	if n, err := s.DeleteExpired(ctx, 100); err != nil || n != 0 {
		t.Errorf("DeleteExpired() within retention = %d, %v", n, err)
	}
	// Shortening the retention applies to stored keys at once.
	retention = time.Hour
	if r := send(h, "POST", "/v1/things", `{"a":2}`, "user:usr_1", "old"); r.Code != http.StatusCreated || r.Header().Get(idempotency.ReplayedHeader) != "" {
		t.Errorf("expired key with another body = %d %v, want a new run", r.Code, r.Header())
	}
	now = now.Add(2 * time.Hour)
	if n, err := s.DeleteExpired(ctx, 100); err != nil || n != 2 {
		t.Errorf("DeleteExpired() after the retention = %d, %v; want 2", n, err)
	}
	if _, ok, err := s.Oldest(ctx); err != nil || ok {
		t.Errorf("Oldest() with no keys = %v, %v", ok, err)
	}
}

// TestStoreUnavailable refuses requests with keys while the database is
// unreachable, rather than running them without protection.
func TestStoreUnavailable(t *testing.T) {
	s, pool := newStore(t)
	pool.Close()
	next := &counting{}
	h := serve(s, next)
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", "down"); r.Code != http.StatusServiceUnavailable || problemCode(r) != "unavailable" {
		t.Errorf("request with the store down = %d %s, want 503 unavailable", r.Code, r.Body)
	}
	if r := send(h, "POST", "/v1/things", `{}`, "user:usr_1", ""); r.Code != http.StatusCreated {
		t.Errorf("request without a key with the store down = %d, want it to run", r.Code)
	}
	if next.runs.Load() != 1 {
		t.Errorf("handler ran %d times, want only the request without a key", next.runs.Load())
	}
}
