package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/actor"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/telemetry"
	"gorbital.dev/requestid"
)

func TestHTTPTraceAndCorrelatedLogs(t *testing.T) {
	ctx := context.Background()
	spans := tracetest.NewInMemoryExporter()
	var logs bytes.Buffer
	tel, err := telemetry.Setup(ctx, "my-api", "v0.1.0",
		telemetry.WithSpanExporter(spans),
		telemetry.WithLogWriter(&logs),
		telemetry.WithoutGlobals(),
		// httptest requests come from 192.0.2.1.
		telemetry.WithTraceContextFrom([]netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}),
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	logger := tel.Logger()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := actor.With(r.Context(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", OrgID: "org_1"})
		logger.InfoContext(ctx, "project loaded", "project_id", r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	h := httpx.Chain(mux, httpx.RequestID(), tel.HTTPMiddleware())

	req := httptest.NewRequest("GET", "/v1/projects/prj_42", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	got := spans.GetSpans()
	if len(got) != 1 {
		t.Fatalf("exported %d spans, want 1", len(got))
	}
	span := got[0]
	if span.Name != "GET /v1/projects/{id}" {
		t.Errorf("span name = %q, want route pattern", span.Name)
	}
	if tid := span.SpanContext.TraceID().String(); tid != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("span trace ID = %s, want incoming traceparent trace ID", tid)
	}

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("log %q is not JSON: %v", logs.String(), err)
	}
	for key, want := range map[string]any{
		"service":    "my-api",
		"trace_id":   "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id":    span.SpanContext.SpanID().String(),
		"org_id":     "org_1",
		"project_id": "prj_42",
	} {
		if line[key] != want {
			t.Errorf("log %s = %v, want %v (line %v)", key, line[key], want, line)
		}
	}
	if rid, _ := line["request_id"].(string); !strings.HasPrefix(rid, "req_") {
		t.Errorf("log request_id = %v, want generated req_ ID", line["request_id"])
	}

	if err := tel.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestHTTPUntrustedTraceContext checks that a client can't choose its trace:
// hide its requests with an unsampled traceparent, force sampling, reuse
// another trace ID, send baggage or forge its address (HTTP-1).
func TestHTTPUntrustedTraceContext(t *testing.T) {
	ctx := context.Background()
	const incoming = "0af7651916cd43dd8448eb211c80319c"
	for name, tt := range map[string]struct {
		ratio     float64
		flags     string
		wantSpans int
	}{
		"unsampled traceparent is still traced":      {ratio: 1, flags: "00", wantSpans: 1},
		"sampled traceparent doesn't force sampling": {ratio: 0, flags: "01", wantSpans: 0},
	} {
		spans := tracetest.NewInMemoryExporter()
		tel, err := telemetry.Setup(ctx, "my-api", "v0.1.0",
			telemetry.WithSpanExporter(spans),
			telemetry.WithLogWriter(io.Discard),
			telemetry.WithSampleRatio(tt.ratio),
			telemetry.WithoutGlobals(),
			// Trusted callers elsewhere don't make this client trusted.
			telemetry.WithTraceContextFrom([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}),
		)
		if err != nil {
			t.Fatalf("Setup() error = %v", err)
		}
		var sawBaggage bool
		h := tel.HTTPMiddleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			sawBaggage = baggage.FromContext(r.Context()).Len() > 0
		}))
		req := httptest.NewRequest("POST", "/v1/auth/login", nil)
		req.Header.Set("traceparent", "00-"+incoming+"-b7ad6b7169203331-"+tt.flags)
		req.Header.Set("baggage", "tenant=forged")
		req.Header.Set("X-Forwarded-For", "203.0.113.99")
		h.ServeHTTP(httptest.NewRecorder(), req)

		got := spans.GetSpans()
		if len(got) != tt.wantSpans {
			t.Fatalf("%s: exported %d spans, want %d", name, len(got), tt.wantSpans)
		}
		if sawBaggage {
			t.Errorf("%s: handler context has the client's baggage", name)
		}
		if len(got) == 0 {
			continue
		}
		span := got[0]
		if span.SpanContext.TraceID().String() == incoming || span.Parent.IsValid() {
			t.Errorf("%s: span trace %s, parent %v; want a new root trace", name, span.SpanContext.TraceID(), span.Parent)
		}
		if len(span.Links) != 1 || span.Links[0].SpanContext.TraceID().String() != incoming {
			t.Errorf("%s: span links = %v, want a link to the incoming trace", name, span.Links)
		}
		for _, a := range span.Attributes {
			if a.Key == "client.address" && a.Value.AsString() != "192.0.2.1" {
				t.Errorf("%s: client.address = %s, want the peer 192.0.2.1, not X-Forwarded-For", name, a.Value.AsString())
			}
		}
	}
}

func TestHTTPTrustedTraceContextKeepsSampling(t *testing.T) {
	spans := tracetest.NewInMemoryExporter()
	tel, err := telemetry.Setup(context.Background(), "my-api", "v0.1.0",
		telemetry.WithSpanExporter(spans),
		telemetry.WithLogWriter(io.Discard),
		telemetry.WithoutGlobals(),
		telemetry.WithTraceContextFrom([]netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")}),
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	var parent trace.SpanContext
	h := tel.HTTPMiddleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		parent = trace.SpanContextFromContext(r.Context())
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-00")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if n := len(spans.GetSpans()); n != 0 || parent.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("trusted unsampled request: %d spans, trace %s; want the caller's unsampled trace", n, parent.TraceID())
	}
}

func TestLogHandlerDoesNotDuplicateKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(telemetry.NewLogHandler(slog.NewTextHandler(&buf, nil)))
	ctx := requestid.With(context.Background(), "req_ctx")

	logger.InfoContext(ctx, "explicit", "request_id", "req_explicit")
	if n := strings.Count(buf.String(), "request_id="); n != 1 || !strings.Contains(buf.String(), "req_explicit") {
		t.Errorf("log %q has %d request_id keys, want 1 explicit", buf.String(), n)
	}

	buf.Reset()
	logger.With("component", "jobs").InfoContext(ctx, "derived")
	if !strings.Contains(buf.String(), "request_id=req_ctx") || !strings.Contains(buf.String(), "component=jobs") {
		t.Errorf("derived logger lost context enrichment: %q", buf.String())
	}
}

// levelBuffer is a text handler taking records at level and above.
type levelBuffer struct {
	slog.Handler
	buf *bytes.Buffer
}

func newLevelBuffer(level slog.Level) levelBuffer {
	var buf bytes.Buffer
	return levelBuffer{Handler: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}), buf: &buf}
}

func TestLogTee(t *testing.T) {
	ctx := requestid.With(context.Background(), "req_tee")
	var out bytes.Buffer
	tee := newLevelBuffer(slog.LevelInfo)
	tel, err := telemetry.Setup(ctx, "tee-test", "v0",
		telemetry.WithoutGlobals(), telemetry.WithLogWriter(&out), telemetry.WithLogFormat(telemetry.LogFormatText),
		telemetry.WithLogLevel(slog.LevelWarn), telemetry.WithLogTee(tee))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tel.Shutdown(ctx) }()

	logger := tel.Logger().With("component", "tee").WithGroup("g")
	logger.DebugContext(ctx, "debug line")
	logger.InfoContext(ctx, "info line", "k", "v")
	logger.WarnContext(ctx, "warn line")

	if s := out.String(); strings.Contains(s, "info line") || !strings.Contains(s, "warn line") {
		t.Errorf("primary output = %q, want the warning only (its own level)", s)
	}
	s := tee.buf.String()
	for _, want := range []string{"info line", "warn line", "request_id=req_tee", "component=tee", "g.k=v"} {
		if !strings.Contains(s, want) {
			t.Errorf("tee output %q lacks %q", s, want)
		}
	}
	if strings.Contains(s, "debug line") {
		t.Errorf("tee output %q has the debug record its handler doesn't take", s)
	}

	// A nil tee changes nothing.
	tel2, err := telemetry.Setup(ctx, "tee-test", "v0", telemetry.WithoutGlobals(), telemetry.WithLogWriter(io.Discard), telemetry.WithLogTee(nil))
	if err != nil {
		t.Fatal(err)
	}
	tel2.Logger().Info("fine")
	_ = tel2.Shutdown(ctx)

	// Several tees each receive every record their level takes.
	first, second := newLevelBuffer(slog.LevelInfo), newLevelBuffer(slog.LevelWarn)
	tel3, err := telemetry.Setup(ctx, "tee-test", "v0", telemetry.WithoutGlobals(), telemetry.WithLogWriter(io.Discard),
		telemetry.WithLogFormat(telemetry.LogFormatText), telemetry.WithLogTee(first), telemetry.WithLogTee(nil), telemetry.WithLogTee(second))
	if err != nil {
		t.Fatal(err)
	}
	tel3.Logger().With("component", "both").InfoContext(ctx, "info both")
	tel3.Logger().WarnContext(ctx, "warn both")
	_ = tel3.Shutdown(ctx)
	if s := first.buf.String(); !strings.Contains(s, "info both") || !strings.Contains(s, "warn both") || !strings.Contains(s, "component=both") {
		t.Errorf("first tee = %q, want both records", s)
	}
	if s := second.buf.String(); strings.Contains(s, "info both") || !strings.Contains(s, "warn both") {
		t.Errorf("second tee = %q, want the warning only", s)
	}
}

func TestSetupValidation(t *testing.T) {
	ctx := context.Background()
	for name, run := range map[string]func() error{
		"empty service": func() error { _, err := telemetry.Setup(ctx, "", "v1", telemetry.WithoutGlobals()); return err },
		"bad ratio": func() error {
			_, err := telemetry.Setup(ctx, "svc", "v1", telemetry.WithSampleRatio(2), telemetry.WithoutGlobals())
			return err
		},
		"bad format": func() error {
			_, err := telemetry.Setup(ctx, "svc", "v1", telemetry.WithLogFormat("xml"), telemetry.WithoutGlobals())
			return err
		},
	} {
		if err := run(); err == nil {
			t.Errorf("Setup(%s) error = nil, want error", name)
		}
	}
}
