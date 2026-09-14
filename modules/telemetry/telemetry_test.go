package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"apistock.dev/actor"
	"apistock.dev/httpx"
	"apistock.dev/modules/telemetry"
	"apistock.dev/requestid"
)

func TestHTTPTraceAndCorrelatedLogs(t *testing.T) {
	ctx := context.Background()
	spans := tracetest.NewInMemoryExporter()
	var logs bytes.Buffer
	tel, err := telemetry.Setup(ctx, "my-api", "v0.1.0",
		telemetry.WithSpanExporter(spans),
		telemetry.WithLogWriter(&logs),
		telemetry.WithoutGlobals(),
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
