package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"apistock.dev/actor"
	"apistock.dev/requestid"
)

// Log attribute keys added from the context.
const (
	KeyRequestID = "request_id"
	KeyTraceID   = "trace_id"
	KeySpanID    = "span_id"
	KeyOrgID     = "org_id"
)

// NewLogHandler wraps h so records logged with a context carry request_id,
// trace_id, span_id and org_id when available. Keys already present on the
// record are not added twice.
func NewLogHandler(h slog.Handler) slog.Handler { return contextHandler{Handler: h} }

type contextHandler struct {
	slog.Handler
}

// Handle adds context attributes and forwards the record.
func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	present := map[string]bool{}
	r.Attrs(func(a slog.Attr) bool {
		present[a.Key] = true
		return true
	})
	add := func(key, value string) {
		if value != "" && !present[key] {
			r.AddAttrs(slog.String(key, value))
		}
	}

	add(KeyRequestID, requestid.From(ctx))
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		add(KeyTraceID, sc.TraceID().String())
		add(KeySpanID, sc.SpanID().String())
	}
	if a, ok := actor.From(ctx); ok {
		add(KeyOrgID, a.OrgID)
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs keeps the context enrichment on derived handlers.
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup keeps the context enrichment on derived handlers.
func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}
