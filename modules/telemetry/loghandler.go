package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/actor"
	"gorbital.dev/requestid"
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

// teeHandler sends records to primary and to tee, each deciding with its
// own Enabled.
type teeHandler struct {
	primary, tee slog.Handler
}

// Enabled reports whether either handler takes records at level.
func (h teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level) || h.tee.Enabled(ctx, level)
}

// Handle passes r to each handler that takes its level. The tee's error is
// ignored: it must never make logging fail.
func (h teeHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.tee.Enabled(ctx, r.Level) {
		_ = h.tee.Handle(ctx, r.Clone())
	}
	if h.primary.Enabled(ctx, r.Level) {
		return h.primary.Handle(ctx, r)
	}
	return nil
}

// WithAttrs adds attrs to both handlers.
func (h teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return teeHandler{primary: h.primary.WithAttrs(attrs), tee: h.tee.WithAttrs(attrs)}
}

// WithGroup opens a group in both handlers.
func (h teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{primary: h.primary.WithGroup(name), tee: h.tee.WithGroup(name)}
}
