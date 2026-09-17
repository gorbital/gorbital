package observability_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorbital.dev/modules/observability"
)

// copiedKey is the key copyRequest stores under: a package-local type, so
// it can't collide with another package's context value.
type copiedKey struct{}

// copyRequest stands in for middleware such as authentication that
// replaces the request with r.WithContext before the router.
func copyRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), copiedKey{}, "copied")))
	})
}

func testMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "missing" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /v1/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	mux.HandleFunc("GET /v1/stream", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	return mux
}

// recoverer turns panics into 500 responses, like httpx.Recover.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func TestMiddlewareRecordsRoutes(t *testing.T) {
	c := newCollector(t)
	h := recoverer(c.Middleware()(copyRequest(observability.RecordRoute(testMux()))))
	send := func(method, target string, headers ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Client-chosen paths, queries, methods and headers never make series.
	for i := range 50 {
		send("GET", fmt.Sprintf("/v1/projects/p%d?token=secret%d", i, i), "Host", fmt.Sprintf("h%d.example", i), "X-Forwarded-For", "203.0.113.9")
		send("GET", fmt.Sprintf("/nope/%d", i))
		send(fmt.Sprintf("M%d", i), "/v1/projects/1")
	}
	send("GET", "/v1/projects/missing")
	send("POST", "/v1/boom")
	if rec := send("GET", "/v1/stream"); rec.Code != http.StatusOK || !rec.Flushed {
		t.Errorf("GET /v1/stream = %d, flushed %v: the recorder hides http.Flusher", rec.Code, rec.Flushed)
	}

	minutes := c.Minutes()
	for _, m := range minutes {
		for _, s := range []string{"secret", "h1.example", "203.0.113", "/nope/", "/v1/projects/p", "M1"} {
			if strings.Contains(m.Route+m.Method, s) {
				t.Errorf("series %s %s holds a client value %q", m.Method, m.Route, s)
			}
		}
	}
	if len(minutes) != 5 {
		t.Errorf("series = %d, want 5 (project, unmatched, other method, boom, stream): %+v", len(minutes), minutes)
	}
	project, _ := find(minutes, "GET", "/v1/projects/{id}")
	if project.Requests != 51 || project.ClientErrors != 1 {
		t.Errorf("GET /v1/projects/{id} = %+v, want 51 requests with one 404", project)
	}
	// "/nope/…" matched no route; with no catch-all pattern the mux's 404
	// has no pattern either.
	if unmatched, _ := find(minutes, "GET", ""); unmatched.Requests != 50 || unmatched.ClientErrors != 50 {
		t.Errorf("unmatched = %+v, want 50 requests, all 404", unmatched)
	}
	if other, _ := find(minutes, observability.OtherMethod, ""); other.Requests != 50 {
		t.Errorf("unknown methods = %+v, want 50 requests under %s", other, observability.OtherMethod)
	}
	if boom, _ := find(minutes, "POST", "/v1/boom"); boom.Requests != 1 || boom.ServerErrors != 1 {
		t.Errorf("panicking handler = %+v, want one server error", boom)
	}
}

func TestMiddlewareWithoutRequestCopies(t *testing.T) {
	c := newCollector(t)
	h := c.Middleware()(testMux()) // no RecordRoute: the pattern is still visible
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/projects/7", nil))
	if _, ok := find(c.Minutes(), "GET", "/v1/projects/{id}"); !ok {
		t.Errorf("series = %+v, want the route without RecordRoute when nothing copies the request", c.Minutes())
	}
	// RecordRoute alone does nothing.
	rec := httptest.NewRecorder()
	observability.RecordRoute(testMux()).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/projects/7", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("RecordRoute without the middleware = %d", rec.Code)
	}
}

// BenchmarkMiddleware measures the collector's cost per request, through
// a request copy and RecordRoute, against the bare handler chain.
func BenchmarkMiddleware(b *testing.B) {
	mux := testMux()
	req := httptest.NewRequest("GET", "/v1/projects/42", nil)
	b.Run("without", func(b *testing.B) {
		h := copyRequest(mux)
		b.ReportAllocs()
		for b.Loop() {
			h.ServeHTTP(discard{}, req)
		}
	})
	b.Run("with", func(b *testing.B) {
		c := newCollector(b)
		h := c.Middleware()(copyRequest(observability.RecordRoute(mux)))
		b.ReportAllocs()
		for b.Loop() {
			h.ServeHTTP(discard{}, req)
		}
	})
	b.Run("parallel", func(b *testing.B) {
		c := newCollector(b)
		h := c.Middleware()(copyRequest(observability.RecordRoute(mux)))
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				h.ServeHTTP(discard{}, req)
			}
		})
	})
}

type discard struct{}

func (discard) Header() http.Header         { return http.Header{} }
func (discard) Write(b []byte) (int, error) { return len(b), nil }
func (discard) WriteHeader(int)             {}

func TestStreams(t *testing.T) {
	s, err := observability.NewStreams(3, 2, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, doneA1, err := s.Open(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	_, doneA2, _ := s.Open(ctx, "a")
	if _, _, err := s.Open(ctx, "a"); !errors.Is(err, observability.ErrTooManyStreams) {
		t.Errorf("third stream for one subject: error = %v, want ErrTooManyStreams", err)
	}
	expiring, doneB, err := s.Open(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(ctx, "c"); !errors.Is(err, observability.ErrTooManyStreams) {
		t.Errorf("fourth stream: error = %v, want ErrTooManyStreams", err)
	}
	<-expiring.Done()
	if cause := context.Cause(expiring); !errors.Is(cause, observability.ErrStreamExpired) {
		t.Errorf("expired stream cause = %v, want ErrStreamExpired", cause)
	}
	doneB()
	doneB() // twice is fine
	doneA2()
	if s.Count() != 1 {
		t.Errorf("Count() = %d, want 1", s.Count())
	}

	longer, _ := observability.NewStreams(3, 2, time.Hour)
	open, done, _ := longer.Open(ctx, "a")
	defer done()
	longer.Close()
	<-open.Done()
	if cause := context.Cause(open); !errors.Is(cause, observability.ErrStreamsClosed) {
		t.Errorf("closed stream cause = %v, want ErrStreamsClosed", cause)
	}
	if _, _, err := longer.Open(ctx, "a"); !errors.Is(err, observability.ErrStreamsClosed) {
		t.Errorf("Open after Close error = %v, want ErrStreamsClosed", err)
	}
	doneA1()
	if _, err := observability.NewStreams(0, 1, time.Second); err == nil {
		t.Error("NewStreams(0, …) error = nil")
	}
}
