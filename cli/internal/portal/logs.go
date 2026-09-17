package portal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Log endpoints (ADR-0072), under /_portal/api/logs:
//
//	GET  logs            the records matching the filters, newest first
//	GET  logs/histogram  counts per bucket
//	GET  logs/stream     new records as Server-Sent Events (live tail)
//	GET  logs/errors     records at WARN and above grouped by fingerprint
//	GET  logs/request/{id}  every record of one request
//	GET  logs/stats      the store's size
//	DELETE logs          clears the store
//	GET  logs/filters, PUT logs/filters, DELETE logs/filters/{name}

// logQuery reads the filters from the query string.
func logQuery(r *http.Request) (LogQuery, error) {
	v := r.URL.Query()
	q := LogQuery{
		MinLevel:    strings.ToUpper(v.Get("min_level")),
		User:        strings.TrimSpace(v.Get("user")),
		Method:      strings.ToUpper(strings.TrimSpace(v.Get("method"))),
		Path:        v.Get("path"),
		StatusClass: v.Get("status_class"),
		RequestID:   v.Get("request_id"),
		TraceID:     v.Get("trace_id"),
		Text:        v.Get("q"),
	}
	list := func(key string) []string {
		var out []string
		for _, s := range strings.Split(v.Get(key), ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	q.Levels, q.Sources = list("level"), list("source")
	var err error
	if s := v.Get("from"); s != "" {
		if q.From, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return q, fmt.Errorf("from: %w", err)
		}
	}
	if s := v.Get("to"); s != "" {
		if q.To, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return q, fmt.Errorf("to: %w", err)
		}
	}
	if s := v.Get("status"); s != "" {
		if q.Status, err = strconv.Atoi(s); err != nil {
			return q, fmt.Errorf("status: %w", err)
		}
	}
	if s := v.Get("min_duration_ms"); s != "" {
		if q.MinDurationMS, err = strconv.ParseFloat(s, 64); err != nil {
			return q, fmt.Errorf("min_duration_ms: %w", err)
		}
	}
	if s := v.Get("before"); s != "" {
		if q.Before, err = strconv.ParseInt(s, 10, 64); err != nil {
			return q, fmt.Errorf("before: %w", err)
		}
	}
	if s := v.Get("after"); s != "" {
		if q.After, err = strconv.ParseInt(s, 10, 64); err != nil {
			return q, fmt.Errorf("after: %w", err)
		}
	}
	if s := v.Get("limit"); s != "" {
		if q.Limit, err = strconv.Atoi(s); err != nil {
			return q, fmt.Errorf("limit: %w", err)
		}
	}
	if q.StatusClass != "" && !strings.HasSuffix(q.StatusClass, "xx") {
		return q, fmt.Errorf("status_class: want 2xx, 3xx, 4xx or 5xx")
	}
	return q, nil
}

// logRoutes registers the endpoints; without a store they answer 404.
func (s *Server) logRoutes(mux *http.ServeMux) {
	guard := func(fn func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Logs == nil {
				writeProblem(w, http.StatusNotFound, "no_log_store", "this orb dev keeps no log store")
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET "+APIPrefix+"logs", guard(s.serveLogs))
	mux.HandleFunc("GET "+APIPrefix+"logs/histogram", guard(s.serveLogHistogram))
	mux.HandleFunc("GET "+APIPrefix+"logs/stream", guard(s.serveLogStream))
	mux.HandleFunc("GET "+APIPrefix+"logs/errors", guard(s.serveLogErrors))
	mux.HandleFunc("GET "+APIPrefix+"logs/request/{id}", guard(s.serveLogRequest))
	mux.HandleFunc("GET "+APIPrefix+"logs/stats", guard(s.serveLogStats))
	mux.HandleFunc("DELETE "+APIPrefix+"logs", guard(s.serveLogClear))
	mux.HandleFunc("GET "+APIPrefix+"logs/filters", guard(s.serveLogFilters))
	mux.HandleFunc("PUT "+APIPrefix+"logs/filters", guard(s.serveLogFilterSave))
	mux.HandleFunc("DELETE "+APIPrefix+"logs/filters/{name}", guard(s.serveLogFilterDelete))
}

func (s *Server) serveLogs(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	page, err := s.cfg.Logs.Query(q)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, page)
}

func (s *Server) serveLogHistogram(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	width := time.Minute
	if b := r.URL.Query().Get("bucket"); b != "" {
		if width, err = time.ParseDuration(b); err != nil || width < time.Second {
			writeProblem(w, http.StatusBadRequest, "invalid_query", "bucket: a duration of at least 1s, such as 1m")
			return
		}
	}
	buckets, err := s.cfg.Logs.Histogram(q, width)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"bucket": width.String(), "buckets": buckets})
}

func (s *Server) serveLogErrors(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if q.From.IsZero() && q.To.IsZero() {
		q.From = time.Now().Add(-24 * time.Hour)
	}
	groups, err := s.cfg.Logs.Errors(q)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) serveLogRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	page, err := s.cfg.Logs.Query(LogQuery{RequestID: id, Limit: MaxLogQuery})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	// Oldest first: a request reads top to bottom.
	for i, j := 0, len(page.Logs)-1; i < j; i, j = i+1, j-1 {
		page.Logs[i], page.Logs[j] = page.Logs[j], page.Logs[i]
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"request_id": id, "logs": page.Logs})
}

func (s *Server) serveLogStats(w http.ResponseWriter, _ *http.Request) {
	st, err := s.cfg.Logs.Stats()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, st)
}

func (s *Server) serveLogClear(w http.ResponseWriter, _ *http.Request) {
	if err := s.cfg.Logs.Clear(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveLogFilters(w http.ResponseWriter, _ *http.Request) {
	filters, err := s.cfg.Logs.Filters()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"filters": filters})
}

func (s *Server) serveLogFilterSave(w http.ResponseWriter, r *http.Request) {
	var f SavedFilter
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&f); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := s.cfg.Logs.SaveFilter(f); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	s.serveLogFilters(w, r)
}

func (s *Server) serveLogFilterDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Logs.DeleteFilter(r.PathValue("name")); err != nil {
		writeProblem(w, http.StatusInternalServerError, "log_store_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveLogStream sends records matching the filters as they are stored,
// as Server-Sent Events ("log"), within the same limits as the event
// stream. Records already stored before the stream started come from GET
// logs; pass their newest ID as after to avoid a gap.
func (s *Server) serveLogStream(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if int(s.streams.Add(1)) > s.cfg.MaxStreams {
		s.streams.Add(-1)
		writeProblem(w, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("at most %d event streams at once; close one", s.cfg.MaxStreams))
		return
	}
	defer s.streams.Add(-1)

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	write := func(event string, data any) error {
		payload, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return err
		}
		return rc.Flush()
	}
	_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := io.WriteString(w, "retry: 3000\n\n"); err != nil {
		return
	}
	records, unsubscribe := s.cfg.Logs.Subscribe()
	defer unsubscribe()
	// Records added between the client's last page and the subscription.
	if q.After > 0 {
		missed, err := s.cfg.Logs.Query(LogQuery{After: q.After, Limit: MaxLogQuery})
		if err == nil {
			for i := len(missed.Logs) - 1; i >= 0; i-- {
				if q.Matches(missed.Logs[i]) {
					if err := write("log", missed.Logs[i]); err != nil {
						return
					}
				}
			}
		}
	}
	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()
	deadline := time.NewTimer(s.cfg.StreamDuration)
	defer deadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closed:
			_ = write("end", StreamEnd{Reason: "shutdown"})
			return
		case <-deadline.C:
			_ = write("end", StreamEnd{Reason: "duration"})
			return
		case <-keepAlive.C:
			_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		case rec := <-records:
			if q.Matches(rec) {
				if err := write("log", rec); err != nil {
					return
				}
			}
		}
	}
}

// RenderText renders a JSON log line as slog's text handler would, for
// the terminal while the app logs JSON for the store; other lines come
// back unchanged.
func RenderText(line string) string {
	if !strings.HasPrefix(line, "{") {
		return line
	}
	r, ok := parseJSONLog(line)
	if !ok {
		return line
	}
	var b strings.Builder
	b.WriteString("time=")
	b.WriteString(r.Time.Local().Format("2006-01-02T15:04:05.000Z07:00"))
	b.WriteString(" level=")
	b.WriteString(r.Level)
	b.WriteString(" msg=")
	b.WriteString(quoteText(r.Message))
	for _, a := range r.Attrs {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(quoteText(a.Value))
	}
	return b.String()
}

// quoteText quotes a value as slog's text handler does: only when it has
// spaces, quotes, or control characters.
func quoteText(s string) string {
	if s == "" {
		return `""`
	}
	for _, c := range s {
		if c <= ' ' || c == '"' || c == '=' || c == 0x7f {
			return strconv.Quote(s)
		}
	}
	return s
}
