package httpx_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorbital.dev/httpx"
	"gorbital.dev/requestid"
)

func problemCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &p)
	return p.Code
}

func TestTimeoutPassesFastResponses(t *testing.T) {
	outer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Outer", "kept")
			w.Header().Set("X-Removed", "by the handler")
			next.ServeHTTP(w, r)
		})
	}
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantBody   string
		wantHeader map[string]string
	}{
		{"status, headers and body", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := r.Context().Deadline(); !ok {
				t.Error("the request context has no deadline")
			}
			if got := w.Header().Get("X-Outer"); got != "kept" {
				t.Errorf("handler sees X-Outer = %q", got)
			}
			w.Header().Set("X-Handler", "set")
			w.Header().Del("X-Removed")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "created")
		}, http.StatusCreated, "created", map[string]string{"X-Outer": "kept", "X-Handler": "set", "X-Removed": ""}},
		{"body without status", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "hello")
		}, http.StatusOK, "hello", map[string]string{"X-Outer": "kept"}},
		{"headers without writing", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Handler", "set")
		}, http.StatusOK, "", map[string]string{"X-Handler": "set", "X-Outer": "kept"}},
		{"second status ignored", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			w.WriteHeader(http.StatusTeapot)
		}, http.StatusAccepted, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpx.Chain(tt.handler, outer, httpx.Timeout(time.Second))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
				t.Errorf("response = %d %q, want %d %q", rec.Code, rec.Body.String(), tt.wantStatus, tt.wantBody)
			}
			for k, v := range tt.wantHeader {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("header %s = %q, want %q", k, got, v)
				}
			}
		})
	}
}

func TestTimeoutRespondsAtTheDeadline(t *testing.T) {
	release := make(chan struct{})
	lateWrite := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Handler", "leaked?")
		<-release // ignores its context
		_, err := io.WriteString(w, "too late")
		w.WriteHeader(http.StatusOK)
		w.Header().Set("X-After", "x")
		if err == nil {
			err = http.NewResponseController(w).Flush()
		}
		lateWrite <- err
	})
	srv := httptest.NewServer(httpx.Chain(handler, httpx.RequestID(), httpx.Timeout(20*time.Millisecond)))
	defer srv.Close()
	defer close(release)

	// The whole 503 reaches an HTTP/1.1 client while the handler still runs.
	start := time.Now()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	elapsed := time.Since(start)
	if err != nil || resp.StatusCode != http.StatusServiceUnavailable || problemCodeOf(t, body) != "request_timeout" {
		t.Fatalf("response = %d %s, %v; want 503 request_timeout", resp.StatusCode, body, err)
	}
	if elapsed > time.Second {
		t.Errorf("response complete after %v, want at the deadline", elapsed)
	}
	if resp.Header.Get(requestid.Header) == "" || resp.Header.Get("Content-Type") != httpx.ProblemContentType || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v, want the request ID, problem content type and no-store", resp.Header)
	}
	var p httpx.Problem
	if json.Unmarshal(body, &p) != nil || p.RequestID != resp.Header.Get(requestid.Header) {
		t.Errorf("problem = %+v, want the request ID %q", p, resp.Header.Get(requestid.Header))
	}
	if resp.Header.Get("X-Handler") != "" {
		t.Error("the handler's headers reached the timeout response")
	}

	release <- struct{}{}
	if err := <-lateWrite; !errors.Is(err, http.ErrHandlerTimeout) {
		t.Errorf("late write error = %v, want http.ErrHandlerTimeout", err)
	}
}

func TestTimeoutLateWritesDiscarded(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond) // ignores its context
		w.Header().Set("X-After", "x")
		w.WriteHeader(http.StatusCreated)
		if _, err := io.WriteString(w, "too late"); !errors.Is(err, http.ErrHandlerTimeout) {
			t.Errorf("late write error = %v", err)
		}
	})
	rec := httptest.NewRecorder()
	httpx.Timeout(5*time.Millisecond)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "too late") || rec.Header().Get("X-After") != "" {
		t.Errorf("response = %d %v %q, want only the 503", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestTimeoutHandlerSeesDeadlineExceeded(t *testing.T) {
	errs := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		errs <- r.Context().Err()
		// What a handler does with a cancelled query: an error response.
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "query cancelled"))
	})
	rec := httptest.NewRecorder()
	httpx.Timeout(10*time.Millisecond)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if err := <-errs; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("handler context error = %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestTimeoutStartedResponseIsNotReplaced(t *testing.T) {
	ctxErr := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "part 1;")
		<-r.Context().Done()
		ctxErr <- r.Context().Err()
		_, _ = io.WriteString(w, "part 2")
	})
	rec := httptest.NewRecorder()
	httpx.Timeout(10*time.Millisecond)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusAccepted || rec.Body.String() != "part 1;part 2" {
		t.Errorf("response = %d %q, want the handler's whole response", rec.Code, rec.Body.String())
	}
	if err := <-ctxErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("context error = %v, want the deadline to cancel the context", err)
	}
}

func TestTimeoutStreamingThroughARealServer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Trailer", "X-Checksum")
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Errorf("SetWriteDeadline() error = %v", err)
		}
		if err := rc.EnableFullDuplex(); err != nil {
			t.Errorf("EnableFullDuplex() error = %v", err)
		}
		for i := range 3 {
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			if err := rc.Flush(); err != nil {
				t.Errorf("Flush() error = %v", err)
			}
			time.Sleep(15 * time.Millisecond) // past the deadline after the first event
		}
		w.Header().Set("X-Checksum", "abc")
	})
	srv := httptest.NewServer(httpx.Timeout(20 * time.Millisecond)(handler))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// The first event arrives before the handler finishes.
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "data: 0\n" {
		t.Fatalf("first line = %q, %v", line, err)
	}
	rest, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(rest), "data: 2") {
		t.Errorf("stream = %d %q, want every event", resp.StatusCode, rest)
	}
	if resp.Trailer.Get("X-Checksum") != "abc" {
		t.Errorf("trailer = %v, want X-Checksum", resp.Trailer)
	}
}

func TestTimeoutHijack(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("Hijack() error = %v", err)
			return
		}
		defer conn.Close()
		time.Sleep(30 * time.Millisecond) // past the deadline: no 503 on a hijacked connection
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 6\r\nConnection: close\r\n\r\ncustom")
		_ = rw.Flush()
	})
	srv := httptest.NewServer(httpx.Timeout(10 * time.Millisecond)(handler))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "custom" {
		t.Errorf("response = %d %q", resp.StatusCode, body)
	}
}

func TestTimeoutResponseControllerAfterTimeout(t *testing.T) {
	results := make(chan []error, 1)
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		rc := http.NewResponseController(w)
		_, _, hijackErr := rc.Hijack()
		unwrapped := w.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
		_, writeErr := unwrapped.Write([]byte("x"))
		results <- []error{
			rc.Flush(), rc.SetReadDeadline(time.Now()), rc.SetWriteDeadline(time.Now()), rc.EnableFullDuplex(), hijackErr, writeErr,
		}
	})
	rec := httptest.NewRecorder()
	time.AfterFunc(30*time.Millisecond, func() { close(release) })
	httpx.Timeout(10*time.Millisecond)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for i, err := range <-results {
		if !errors.Is(err, http.ErrHandlerTimeout) {
			t.Errorf("call %d after the timeout: error = %v, want http.ErrHandlerTimeout", i, err)
		}
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Body.Len() == 0 || bytes.Contains(rec.Body.Bytes(), []byte("x\n")) {
		t.Errorf("response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestTimeoutClientGoneWaitsForTheHandler(t *testing.T) {
	var finished atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		time.Sleep(10 * time.Millisecond)
		finished.Store(true)
	})
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	httpx.Timeout(time.Minute)(handler).ServeHTTP(rec, req)
	if !finished.Load() {
		t.Error("returned before the handler of a cancelled request finished")
	}
	if rec.Code == http.StatusServiceUnavailable {
		t.Error("answered 503 to a request whose client went away")
	}
}

func TestTimeoutEarlyHints(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", "</app.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		<-r.Context().Done()
	})
	rec := httptest.NewRecorder()
	httpx.Timeout(10*time.Millisecond)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	// httptest.ResponseRecorder keeps the first status, the 103; what
	// matters is that an informational response doesn't block the 503.
	if !strings.Contains(rec.Body.String(), "request_timeout") {
		t.Errorf("body = %q, want the timeout problem after early hints", rec.Body.String())
	}
}

func TestTimeoutPanics(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom in handler")
	}), httpx.Recover(logger), httpx.Timeout(time.Second))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError || problemCodeOf(t, rec.Body.Bytes()) != "internal_error" {
		t.Errorf("response = %d %s, want 500 internal_error", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "boom in handler") || !strings.Contains(logs.String(), "timeout_test.go") {
		t.Errorf("log lacks the panic and the handler's stack: %s", logs.String())
	}

	// http.ErrAbortHandler is re-raised as is.
	func() {
		defer func() {
			if v := recover(); v != http.ErrAbortHandler { //nolint:errorlint // the value must be the sentinel itself
				t.Errorf("recovered %v, want http.ErrAbortHandler", v)
			}
		}()
		httpx.Timeout(time.Second)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	// A panic after the 503 was sent is recovered and logged; the 503 stays.
	logs.Reset()
	rec = httptest.NewRecorder()
	httpx.Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		time.Sleep(5 * time.Millisecond)
		panic("after the timeout")
	}), httpx.Recover(logger), httpx.Timeout(5*time.Millisecond)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(logs.String(), "after the timeout") {
		t.Errorf("status = %d, log %q; want 503 and the panic logged", rec.Code, logs.String())
	}
}

func TestTimeoutDisabled(t *testing.T) {
	var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, d := range []time.Duration{0, -time.Second} {
		if got := httpx.Timeout(d)(h); fmt.Sprintf("%p", got) != fmt.Sprintf("%p", h) {
			t.Errorf("Timeout(%v) wrapped the handler, want it unchanged", d)
		}
	}
}

// TestTimeoutRace runs handlers that write continuously across the
// deadline, for the race detector: every response is either the handler's
// or the 503, never a mix.
func TestTimeoutRace(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; ; i++ {
			if r.Context().Err() != nil {
				return
			}
			w.Header().Set("X-Count", fmt.Sprint(i))
			if i == 3 && r.URL.Query().Get("write") == "1" {
				_, _ = io.WriteString(w, "started")
			}
			time.Sleep(time.Millisecond)
		}
	})
	h := httpx.Timeout(2 * time.Millisecond)(handler)
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?write=%d", i%2), nil))
			body := rec.Body.String()
			switch {
			case rec.Code == http.StatusServiceUnavailable && strings.Contains(body, "request_timeout") && !strings.Contains(body, "started"):
			case rec.Code == http.StatusOK && body == "started":
			default:
				t.Errorf("response = %d %q", rec.Code, body)
			}
		})
	}
	wg.Wait()
}

func BenchmarkTimeout(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	for _, bm := range []struct {
		name string
		h    http.Handler
	}{
		{"without", handler},
		{"with", httpx.Timeout(time.Minute)(handler)},
	} {
		b.Run(bm.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			b.ReportAllocs()
			for b.Loop() {
				rec := httptest.NewRecorder()
				rec.Header().Set(requestid.Header, "req_1")
				bm.h.ServeHTTP(rec, req)
			}
		})
	}
}
