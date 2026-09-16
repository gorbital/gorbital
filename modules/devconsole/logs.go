package devconsole

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

const (
	// maxMessageLength bounds a log record's message.
	maxMessageLength = 4096
	// maxAttrs bounds a log record's attributes; more are counted in
	// Log.DroppedAttrs.
	maxAttrs = 50
	// maxAttrLength bounds an attribute's key and value.
	maxAttrLength = 1024
	// maxRecordBytes bounds a record's message and attributes together;
	// attributes past it are dropped and counted, so the buffer's memory
	// stays within DefaultMaxLogs × 8 KiB.
	maxRecordBytes = 8 << 10
)

// Log is one log record.
type Log struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	// Attrs are the record's attributes in order, groups flattened into
	// dotted keys ("request.id"), values as text. DroppedAttrs counts those
	// left out: past 50, or past 8 KiB for the whole record.
	Attrs        []Attr `json:"attrs"`
	DroppedAttrs int    `json:"dropped_attrs,omitempty"`
}

// Attr is a log attribute.
type Attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Logs keeps an app's most recent log records at info level and above, for
// the console. Its [Logs.Handler] receives them, usually as a tee of the
// app's logger (gorbital.dev/modules/telemetry's WithLogTee). It is safe
// for concurrent use.
type Logs struct {
	buf *buffer[Log]
}

// NewLogs returns a buffer of the n most recent records.
func NewLogs(n int) (*Logs, error) {
	if n < 1 {
		return nil, errors.New("devconsole: the maximum number of log records must be positive")
	}
	return &Logs{buf: newBuffer[Log](n)}, nil
}

// Handler returns the handler that stores records. It takes records at
// info level and above, whatever the logger's own level. A nil *Logs
// returns nil.
func (l *Logs) Handler() slog.Handler {
	if l == nil {
		return nil
	}
	return &logHandler{logs: l}
}

// List returns the stored records, oldest first.
func (l *Logs) List() []Log { return l.buf.list() }

// logHandler converts records into [Log] values. Values are resolved (so
// slog.LogValuer types such as gorbital.dev/config.Secret print as they
// log) and bounded in length.
type logHandler struct {
	logs   *Logs
	prefix string // open groups, "a.b."
	attrs  []Attr // from WithAttrs, already prefixed
}

// Enabled reports whether level is info or above.
func (h *logHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

// Handle stores r.
func (h *logHandler) Handle(_ context.Context, r slog.Record) error {
	entry := Log{
		Time:    r.Time.UTC(),
		Level:   r.Level.String(),
		Message: truncate(r.Message, maxMessageLength),
		Attrs:   make([]Attr, 0, min(len(h.attrs)+r.NumAttrs(), maxAttrs)),
	}
	size := len(entry.Message)
	add := func(a Attr) {
		if len(entry.Attrs) >= maxAttrs || size+len(a.Key)+len(a.Value) > maxRecordBytes {
			entry.DroppedAttrs++
			return
		}
		size += len(a.Key) + len(a.Value)
		entry.Attrs = append(entry.Attrs, a)
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		flatten(h.prefix, a, add)
		return true
	})
	h.logs.buf.add(entry)
	return nil
}

// WithAttrs returns a handler adding attrs to every record.
func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &logHandler{logs: h.logs, prefix: h.prefix, attrs: append([]Attr(nil), h.attrs...)}
	for _, a := range attrs {
		flatten(h.prefix, a, func(a Attr) {
			if len(next.attrs) < maxAttrs {
				next.attrs = append(next.attrs, a)
			}
		})
	}
	return next
}

// WithGroup returns a handler whose later attributes are in group name.
func (h *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &logHandler{logs: h.logs, prefix: h.prefix + name + ".", attrs: h.attrs}
}

// flatten passes a to add as text, groups as dotted keys.
func flatten(prefix string, a slog.Attr, add func(Attr)) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		group := v.Group()
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range group {
			flatten(p, g, add)
		}
		return
	}
	if a.Key == "" && v.Kind() == slog.KindAny && v.Any() == nil {
		return // empty attributes are ignored, as slog's handlers do
	}
	add(Attr{Key: truncate(prefix+a.Key, maxAttrLength), Value: truncate(valueText(v), maxAttrLength)})
}

func valueText(v slog.Value) string {
	switch v.Kind() {
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return err.Error()
		}
	}
	return strings.ToValidUTF8(v.String(), "\uFFFD")
}
