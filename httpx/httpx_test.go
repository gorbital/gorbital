package httpx_test

import (
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
	"testing"
	"time"

	"gorbital.dev/httpx"
	"gorbital.dev/requestid"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != httpx.ProblemContentType {
		t.Errorf("Content-Type = %q, want %s", ct, httpx.ProblemContentType)
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body %q is not a problem: %v", rec.Body.String(), err)
	}
	return p
}

func TestChainOrder(t *testing.T) {
	var order []string
	mw := func(name string) httpx.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	serve(httpx.Chain(ok, mw("outer"), mw("inner")), httptest.NewRequest("GET", "/", nil))
	if got := strings.Join(order, ","); got != "outer,inner" {
		t.Errorf("Chain order = %s, want outer,inner", got)
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	h := httpx.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestid.From(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(requestid.Header, "client-abc-123")
	rec := serve(h, req)
	if seen != "client-abc-123" || rec.Header().Get(requestid.Header) != "client-abc-123" {
		t.Errorf("valid incoming ID: context %q, header %q; want client-abc-123", seen, rec.Header().Get(requestid.Header))
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set(requestid.Header, "bad\nvalue")
	rec = serve(h, req)
	if !strings.HasPrefix(seen, "req_") || rec.Header().Get(requestid.Header) != seen {
		t.Errorf("invalid incoming ID: context %q, header %q; want generated req_ ID", seen, rec.Header().Get(requestid.Header))
	}
}

func TestRecover(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })

	rec := serve(httpx.Chain(panicking, httpx.RequestID(), httpx.Recover(logger)), httptest.NewRequest("GET", "/", nil))

	p := decodeProblem(t, rec)
	if rec.Code != 500 || p.Code != "internal_error" || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("Recover() response = %d %+v, want 500 internal_error without panic details", rec.Code, p)
	}
	if !strings.Contains(logs.String(), "boom") || !strings.Contains(logs.String(), p.RequestID) {
		t.Errorf("Recover() log %q, want panic value and request ID", logs.String())
	}

	abort := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("Recover() with ErrAbortHandler: recovered %v, want http.ErrAbortHandler re-panicked", err)
		}
	}()
	serve(httpx.Recover(logger)(abort), httptest.NewRequest("GET", "/", nil))
}

func TestSecureHeaders(t *testing.T) {
	rec := serve(httpx.SecureHeaders(httpx.SecureHeadersOptions{HSTSMaxAge: 365 * 24 * time.Hour})(ok), httptest.NewRequest("GET", "/", nil))
	want := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	rec = serve(httpx.SecureHeaders(httpx.SecureHeadersOptions{})(ok), httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS without HSTSMaxAge = %q, want unset", got)
	}
}

func TestCORS(t *testing.T) {
	if _, err := httpx.CORS(httpx.CORSOptions{AllowedOrigins: []string{"*"}, AllowCredentials: true}); err == nil {
		t.Error("CORS(wildcard + credentials) error = nil, want error")
	}
	if _, err := httpx.CORS(httpx.CORSOptions{AllowedOrigins: []string{"https://app.example.com/"}}); err == nil {
		t.Error("CORS(origin with trailing slash) error = nil, want error")
	}

	mw, err := httpx.CORS(httpx.CORSOptions{AllowedOrigins: []string{"https://app.example.com"}, AllowCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	h := mw(ok)

	req := httptest.NewRequest("GET", "/v1/me", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := serve(h, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("allowed origin headers = %v, want allow-origin and credentials", rec.Header())
	}

	req = httptest.NewRequest("GET", "/v1/me", nil)
	req.Header.Set("Origin", "https://evil.example")
	if rec := serve(h, req); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("disallowed origin got Access-Control-Allow-Origin %q, want none", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	req = httptest.NewRequest("OPTIONS", "/v1/projects", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec = serve(h, req)
	if rec.Code != http.StatusNoContent || !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("preflight = %d methods %q, want 204 with POST", rec.Code, rec.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestCrossOrigin(t *testing.T) {
	mw, err := httpx.CrossOrigin()
	if err != nil {
		t.Fatal(err)
	}
	h := httpx.Chain(ok, httpx.RequestID(), mw)

	tests := []struct {
		name     string
		method   string
		headers  map[string]string
		wantCode int
	}{
		{"cross-site browser POST", "POST", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-origin browser POST", "POST", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusNoContent},
		{"non-browser client POST", "POST", nil, http.StatusNoContent},
		{"cross-site GET", "GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "https://api.example.com/v1/projects", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := serve(h, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("%s = %d, want %d", tt.name, rec.Code, tt.wantCode)
			}
			if tt.wantCode == http.StatusForbidden {
				if p := decodeProblem(t, rec); p.Code != "cross_origin_request_denied" || p.RequestID == "" {
					t.Errorf("denied problem = %+v, want code cross_origin_request_denied with request ID", p)
				}
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := httpx.BodyLimit(10)(echo)

	if rec := serve(h, httptest.NewRequest("POST", "/", strings.NewReader("small"))); rec.Code != http.StatusNoContent {
		t.Errorf("small body = %d, want 204", rec.Code)
	}
	rec := serve(h, httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 11))))
	if p := decodeProblem(t, rec); rec.Code != http.StatusRequestEntityTooLarge || p.Code != "request_too_large" {
		t.Errorf("declared oversized body = %d %+v, want 413 request_too_large", rec.Code, p)
	}
	req := httptest.NewRequest("POST", "/", io.NopCloser(strings.NewReader(strings.Repeat("x", 11))))
	req.ContentLength = -1
	if rec := serve(h, req); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("undeclared oversized body = %d, want 413 while reading", rec.Code)
	}
}

func TestAccessLog(t *testing.T) {
	var logs bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short"))
	})
	h := httpx.Chain(mux, httpx.RequestID(), httpx.AccessLog(slog.New(slog.NewJSONHandler(&logs, nil))))
	serve(h, httptest.NewRequest("GET", "/v1/projects/prj_1?email=ada@example.com", nil))

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("log %q is not JSON: %v", logs.String(), err)
	}
	if line["route"] != "GET /v1/projects/{id}" || line["status"] != float64(418) || line["bytes"] != float64(5) || line["request_id"] == "" {
		t.Errorf("access log = %v, want route pattern, status 418, 5 bytes and request_id", line)
	}
	if strings.Contains(logs.String(), "ada@example.com") {
		t.Errorf("access log contains the query string: %s", logs.String())
	}
}

var errNameTaken = errors.New("project name is already taken")

func TestMapper(t *testing.T) {
	var logs bytes.Buffer
	m, err := httpx.NewMapper(slog.New(slog.NewJSONHandler(&logs, nil)),
		httpx.Mapping{Err: errNameTaken, Status: http.StatusConflict, Code: "project_name_taken"},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestid.With(context.Background(), "req_1")

	p := m.Problem(ctx, fmt.Errorf("create project: %w", errNameTaken))
	if p.Status != 409 || p.Code != "project_name_taken" || p.Detail != errNameTaken.Error() {
		t.Errorf("Problem(wrapped mapped error) = %+v, want 409 project_name_taken", p)
	}

	p = m.Problem(ctx, errors.New("pq: connection to 10.0.0.5 refused"))
	if p.Status != 500 || p.Code != "internal_error" || strings.Contains(p.Detail, "10.0.0.5") {
		t.Errorf("Problem(unmapped error) = %+v, want generic 500", p)
	}
	if !strings.Contains(logs.String(), "10.0.0.5") || !strings.Contains(logs.String(), "req_1") {
		t.Errorf("unmapped error log = %q, want error and request ID", logs.String())
	}

	direct := httpx.NewProblem(http.StatusTooManyRequests, "rate_limited", "slow down")
	if p := m.Problem(ctx, fmt.Errorf("wrapped: %w", direct)); p.Code != "rate_limited" {
		t.Errorf("Problem(wrapped *Problem) = %+v, want the problem itself", p)
	}

	for _, bad := range []httpx.Mapping{
		{Err: nil, Status: 400, Code: "x"},
		{Err: errors.New("a"), Status: 200, Code: "ok"},
		{Err: errors.New("b"), Status: 400, Code: "Not-Snake"},
		{Err: errNameTaken, Status: 409, Code: "other_code"},
		{Err: errors.New("c"), Status: 422, Code: "project_name_taken"}, // a shared code needs the same status
	} {
		if err := m.Add(bad); err == nil {
			t.Errorf("Add(%+v) = nil, want error", bad)
		}
	}
}

func TestServerRunAndShutdown(t *testing.T) {
	inFlight := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httpx.NewServer("127.0.0.1:0", slow, httpx.WithShutdownTimeout(5*time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx) }()

	addrCtx, addrCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer addrCancel()
	addr, err := srv.Addr(addrCtx)
	if err != nil {
		t.Fatalf("Addr() error = %v", err)
	}

	respc := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			respc <- 0
			return
		}
		resp.Body.Close()
		respc <- resp.StatusCode
	}()
	<-inFlight
	cancel() // shutdown while a request is in flight

	if code := <-respc; code != http.StatusNoContent {
		t.Errorf("in-flight request during shutdown = %d, want 204", code)
	}
	if err := <-errc; err != nil {
		t.Errorf("Run() after shutdown = %v, want nil", err)
	}
}
