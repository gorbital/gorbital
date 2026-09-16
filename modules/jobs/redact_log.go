package jobs

import (
	"context"
	"log/slog"

	"gorbital.dev/mail"
)

// redactingHandler removes email addresses from River's log records: River
// logs the error text of failed attempts itself, and providers' replies can
// quote recipients. Logs carry IDs, not addresses (threat 19).
type redactingHandler struct {
	inner slog.Handler
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, mail.RedactAddresses(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return redactingHandler{inner: h.inner.WithAttrs(redacted)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{inner: h.inner.WithGroup(name)}
}

// redactAttr redacts strings, errors and stringers, inside groups too.
func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, mail.RedactAddresses(v.String()))
	case slog.KindGroup:
		group := v.Group()
		redacted := make([]any, len(group))
		for i, g := range group {
			redacted[i] = redactAttr(g)
		}
		return slog.Group(a.Key, redacted...)
	case slog.KindAny:
		switch x := v.Any().(type) {
		case error:
			return slog.String(a.Key, mail.RedactAddresses(x.Error()))
		case interface{ String() string }:
			return slog.String(a.Key, mail.RedactAddresses(x.String()))
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
