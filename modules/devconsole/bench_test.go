package devconsole_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorbital.dev/modules/devconsole"
)

func BenchmarkRecordRequest(b *testing.B) {
	c, err := devconsole.New(token)
	if err != nil {
		b.Fatal(err)
	}
	r := devconsole.Request{Time: time.Now(), Method: "GET", Route: "/v1/projects/{id}", Path: "/v1/projects/prj_123", Status: 200, DurationMS: 1.2, RequestID: "req_0123456789abcdef", TraceID: "0123456789abcdef0123456789abcdef"}
	b.ReportAllocs()
	for b.Loop() {
		c.RecordRequest(r)
	}
}

func BenchmarkMiddleware(b *testing.B) {
	c, err := devconsole.New(token)
	if err != nil {
		b.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/items/{id}", func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/v1/items/1", nil)
	for name, h := range map[string]http.Handler{"without": mux, "with": c.Middleware()(devconsole.RecordRoute(mux))} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}

func BenchmarkLogHandler(b *testing.B) {
	logs, err := devconsole.NewLogs(devconsole.DefaultMaxLogs)
	if err != nil {
		b.Fatal(err)
	}
	logger := slog.New(logs.Handler()).With("service", "acme-api")
	ctx := context.Background()
	failure := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		logger.LogAttrs(ctx, slog.LevelInfo, "http request",
			slog.String("method", "GET"), slog.String("route", "GET /v1/ping"), slog.Int("status", 200),
			slog.Int64("duration_ms", 1), slog.String("request_id", "req_0123456789abcdef"), slog.Any("error", failure))
	}
}
