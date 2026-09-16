package telemetry_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"gorbital.dev/modules/telemetry"
)

// scrape returns the response of a GET /metrics against h.
func scrape(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec
}

// series returns the lines of a Prometheus text exposition that are samples
// of the metric name (with any labels), leaving out comments and other
// metrics whose names start with name.
func series(body, name string) []string {
	var lines []string
	for line := range strings.Lines(body) {
		if strings.HasPrefix(line, name+"{") || strings.HasPrefix(line, name+" ") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

func TestMetricsHandlerOffByDefault(t *testing.T) {
	ctx := context.Background()
	tel, err := telemetry.Setup(ctx, "my-api", "v1.1.0", telemetry.WithLogWriter(io.Discard), telemetry.WithoutGlobals())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown(ctx) })
	if rec := scrape(t, tel.MetricsHandler()); rec.Code != http.StatusNotFound {
		t.Errorf("MetricsHandler() without WithPrometheus = %d, want 404", rec.Code)
	}
}

func TestPrometheusScrape(t *testing.T) {
	ctx := context.Background()
	tel, err := telemetry.Setup(ctx, "my-api", "v1.1.0",
		telemetry.WithLogWriter(io.Discard),
		telemetry.WithoutGlobals(),
		telemetry.WithPrometheus(true),
		telemetry.WithRuntimeMetrics(),
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown(ctx) })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := tel.HTTPMiddleware()(mux)
	for _, id := range []string{"prj_1", "prj_2", "prj_3"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/projects/"+id, nil))
	}

	rec := scrape(t, tel.MetricsHandler())
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("scrape = %d %s, want 200 text/plain", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()

	counts := series(body, "http_server_request_duration_seconds_count")
	if len(counts) != 1 || !strings.Contains(counts[0], `http_route="/v1/projects/{id}"`) || !strings.HasSuffix(counts[0], " 3") {
		t.Errorf("http_server_request_duration_seconds_count = %q, want one series for the route pattern with 3 requests", counts)
	}
	for _, name := range []string{"go_goroutine_count", "go_memory_used_bytes", "go_processor_limit", "target_info"} {
		if len(series(body, name)) == 0 {
			t.Errorf("scrape has no %s metric:\n%s", name, body)
		}
	}
	if !strings.Contains(body, `service_name="my-api"`) {
		t.Errorf("target_info doesn't name the service:\n%s", series(body, "target_info"))
	}
}

// TestPrometheusIgnoresClientValues checks that nothing a client sends turns
// into metric labels: each request below varies the Host header, method,
// path, forwarding and trace headers and user agent, and the scrape still
// has one HTTP series per route and status, without any of those values
// (ADR-0063, HTTP-2).
func TestPrometheusIgnoresClientValues(t *testing.T) {
	ctx := context.Background()
	tel, err := telemetry.Setup(ctx, "my-api", "v1.1.0",
		telemetry.WithLogWriter(io.Discard),
		telemetry.WithoutGlobals(),
		telemetry.WithPrometheus(true),
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown(ctx) })

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := tel.HTTPMiddleware()(mux)
	const n = 100
	for i := range n {
		for _, path := range []string{fmt.Sprintf("/v1/projects/prj_secret%d", i), fmt.Sprintf("/unknown/secret%d", i)} {
			req := httptest.NewRequest(fmt.Sprintf("SECRET%d", i), path+"?q=secret", nil)
			req.Host = fmt.Sprintf("secret%d.attacker.example:%d", i, 1000+i)
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
			req.Header.Set("User-Agent", fmt.Sprintf("secret-agent/%d", i))
			req.Header.Set("traceparent", fmt.Sprintf("00-0af7651916cd43dd8448eb211c8%05d-b7ad6b7169203331-01", i))
			req.Header.Set("baggage", fmt.Sprintf("tenant=secret%d", i))
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	}

	body := scrape(t, tel.MetricsHandler()).Body.String()
	for _, leak := range []string{"secret", "attacker", "203.0.113", "0af7651916cd43dd8448eb211c8"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("scrape contains client value %q:\n%s", leak, body)
		}
	}
	// One series for the route (204) and one for unmatched paths (404), both
	// with the method normalised to _OTHER.
	counts := series(body, "http_server_request_duration_seconds_count")
	if len(counts) != 2 {
		t.Errorf("http_server_request_duration_seconds_count has %d series for %d distinct requests, want 2:\n%s", len(counts), 2*n, strings.Join(counts, "\n"))
	}
	for _, c := range counts {
		if !strings.Contains(c, `http_request_method="_OTHER"`) || !strings.HasSuffix(c, fmt.Sprintf(" %d", n)) {
			t.Errorf("series %s, want method _OTHER and %d requests", c, n)
		}
	}
}

// TestRecordRouteThroughRequestCopies checks that the route reaches spans
// and metrics when middleware between HTTPMiddleware and the mux replaces
// the request, as authentication middleware does with r.WithContext.
func TestRecordRouteThroughRequestCopies(t *testing.T) {
	ctx := context.Background()
	type key struct{}
	copying := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, "principal")))
		})
	}
	for name, tt := range map[string]struct {
		recordRoute bool
		wantSpan    string
		wantLabel   bool
	}{
		"with RecordRoute":    {recordRoute: true, wantSpan: "GET /v1/projects/{id}", wantLabel: true},
		"without RecordRoute": {recordRoute: false, wantSpan: "GET", wantLabel: false},
	} {
		spans := tracetest.NewInMemoryExporter()
		tel, err := telemetry.Setup(ctx, "my-api", "v1.1.0",
			telemetry.WithLogWriter(io.Discard),
			telemetry.WithoutGlobals(),
			telemetry.WithSpanExporter(spans),
			telemetry.WithPrometheus(true),
		)
		if err != nil {
			t.Fatalf("Setup() error = %v", err)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		var router http.Handler = mux
		if tt.recordRoute {
			router = telemetry.RecordRoute(mux)
		}
		h := tel.HTTPMiddleware()(copying(router))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/projects/prj_1", nil))

		var names []string
		for _, s := range spans.GetSpans() {
			names = append(names, s.Name)
		}
		if len(names) != 1 || names[0] != tt.wantSpan {
			t.Errorf("%s: spans %q, want one named %q", name, names, tt.wantSpan)
		}
		counts := series(scrape(t, tel.MetricsHandler()).Body.String(), "http_server_request_duration_seconds_count")
		if len(counts) != 1 || strings.Contains(counts[0], `http_route="/v1/projects/{id}"`) != tt.wantLabel {
			t.Errorf("%s: http_server_request_duration_seconds_count = %q, want http_route: %v", name, counts, tt.wantLabel)
		}
		_ = tel.Shutdown(ctx)
	}
}
