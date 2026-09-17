package portal

import (
	"fmt"
	"strconv"
	"strings"
)

// RenderPretty renders a JSON log line for people: the time, the level,
// the message and the attributes that matter, with the request records
// summarised on one line. Lines that aren't log records come back as
// they are. With color, ANSI colours mark the level and the status.
//
//	04:23:35 INFO  GET /ops/settings → 200 · 2 ms · 13 KB · req_1ea7d5be455144b9
//	04:23:35 WARN  job ran  job=heartbeat job_id=7 attempt=2
func RenderPretty(line string, color bool) string {
	if !strings.HasPrefix(line, "{") {
		return line
	}
	r, ok := parseJSONLog(line)
	if !ok {
		return line
	}
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	var b strings.Builder
	b.WriteString(paint("2", r.Time.Local().Format("15:04:05")))
	b.WriteByte(' ')
	level := r.Level
	if level == "" {
		level = "INFO"
	}
	switch level {
	case "ERROR":
		b.WriteString(paint("31;1", "ERROR"))
	case "WARN":
		b.WriteString(paint("33;1", "WARN "))
	case "DEBUG":
		b.WriteString(paint("2", "DEBUG"))
	default:
		b.WriteString(paint("32", "INFO "))
	}
	b.WriteByte(' ')
	skip := map[string]bool{"service": true, "source": true, "trace_id": true, "span_id": true}
	if r.Message == "http request" && r.Attr("method") != "" {
		status := r.Attr("status")
		code, _ := strconv.Atoi(status)
		statusColor := "32"
		switch {
		case code >= 500:
			statusColor = "31;1"
		case code >= 400:
			statusColor = "33"
		case code >= 300:
			statusColor = "36"
		}
		b.WriteString(paint("1", r.Attr("method")+" "+r.Attr("path")))
		b.WriteString(" → " + paint(statusColor, status))
		if d := r.Attr("duration_ms"); d != "" {
			b.WriteString(" · " + d + " ms")
		}
		if n, err := strconv.ParseInt(r.Attr("bytes"), 10, 64); err == nil && n > 0 {
			b.WriteString(" · " + humanBytes(n))
		}
		if id := r.Attr("request_id"); id != "" {
			b.WriteString(" · " + paint("2", id))
		}
		if route := r.Attr("route"); route != "" && route != r.Attr("path") && !strings.HasSuffix(route, " "+r.Attr("path")) {
			b.WriteString(" · " + paint("2", "route "+route))
		}
		for _, k := range []string{"method", "path", "route", "status", "duration_ms", "bytes", "request_id"} {
			skip[k] = true
		}
	} else {
		b.WriteString(r.Message)
	}
	for _, a := range r.Attrs {
		if skip[a.Key] {
			continue
		}
		b.WriteString("  " + paint("2", a.Key+"=") + quoteText(a.Value))
	}
	return b.String()
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return strconv.FormatInt(n, 10) + " B"
}

// Writer returns a writer that ingests what is written to it as lines of
// stream; partial lines wait for their newline.
func (s *LogStore) Writer(stream string) *logLineWriter {
	return &logLineWriter{store: s, stream: stream}
}

type logLineWriter struct {
	store  *LogStore
	stream string
	buf    []byte
}

func (w *logLineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		w.store.Ingest(w.stream, string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLogLineBytes {
		w.store.Ingest(w.stream, string(w.buf))
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

// FollowOrb ingests orb dev's own messages from the hub (the app's output
// reaches the store through Writer, before it is rendered for people).
func (s *LogStore) FollowOrb(hub *Hub, stop <-chan struct{}) {
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)
	for {
		select {
		case <-stop:
			return
		case e := <-sub.C:
			if e.Type == "output" && e.Output != nil && e.Output.Stream != "app" {
				s.Ingest(e.Output.Stream, e.Output.Text)
			}
		}
	}
}
