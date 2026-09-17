package portal

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// State is what the app under orb dev is doing.
type State string

// States of the app.
const (
	// StatePreparing: orb dev is starting services, migrating and seeding.
	StatePreparing State = "preparing"
	// StateBuilding: go build runs; the previous version may still serve.
	StateBuilding State = "building"
	// StateRunning: the app process is up.
	StateRunning State = "running"
	// StateStopped: the app isn't running (it exited, failed to start, or
	// was stopped from the portal) and orb dev waits for changes or a start.
	StateStopped State = "stopped"
)

// AppStatus describes the app process.
type AppStatus struct {
	State State `json:"state"`
	// PID of the app process while it runs.
	PID int `json:"pid,omitempty"`
	// Addr is APP_ADDR as the app listens on it; URL is how to reach it from
	// this machine.
	Addr string `json:"addr"`
	URL  string `json:"url"`
	// StartedAt is when the current process started.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// Restarts counts starts after the first.
	Restarts int `json:"restarts"`
	// Problem is the last build or migration failure, until the next
	// success. The app may keep running the previous version meanwhile.
	Problem string `json:"problem,omitempty"`
	// Console reports whether the app serves the dev console APIs under
	// /_dev/ (ADR-0065), which the portal proxies.
	Console bool `json:"console"`
}

// Supervisor controls the app process. orb dev implements it; tests use
// fakes. Methods return quickly: a restart is queued, and its progress
// arrives as state events.
type Supervisor interface {
	Status() AppStatus
	// Restart rebuilds the app and starts the new build.
	Restart() error
	// Stop ends the app process and leaves it stopped until Start or a
	// change to its files.
	Stop() error
	// Start starts the app when it is stopped, without rebuilding.
	Start() error
	// Migrate applies the app's pending migrations (go run ./cmd/migrate)
	// without restarting it; apps without a database refuse.
	Migrate() error
	// MigrateDown rolls back the most recent migration (go run ./cmd/migrate
	// --down); MigrateRedo rolls it back and applies it again (--redo), the
	// check that a migration's Down works (ADR-0069).
	MigrateDown() error
	MigrateRedo() error
	// ResetDatabase applies every migration and the seed data again after
	// the portal dropped the schema (ADR-0077).
	ResetDatabase() error
}

// OutputLine is one line the app or orb wrote.
type OutputLine struct {
	Time time.Time `json:"time"`
	// Stream is "app" (the app's standard output and error) or "orb" (orb
	// dev's own messages).
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

// Event is what the portal streams to the UI: a state change, an output
// line or a schema status (ADR-0080).
type Event struct {
	// Type is "state", "output" or "schema".
	Type   string        `json:"type"`
	Time   time.Time     `json:"time"`
	State  *AppStatus    `json:"state,omitempty"`
	Output *OutputLine   `json:"output,omitempty"`
	Schema *SchemaStatus `json:"schema,omitempty"`
}

// Limits of the output buffer.
const (
	// DefaultMaxLines is how many output lines a Hub keeps without
	// [NewHubSize].
	DefaultMaxLines = 2000
	// maxLineBytes cuts longer lines, so one runaway line can't fill memory.
	maxLineBytes = 8 << 10
	// subscriberBuffer is how many events wait for a slow subscriber before
	// newer ones are dropped and counted.
	subscriberBuffer = 256
)

// Hub keeps the most recent output lines and hands every output line and
// state change to subscribers without ever blocking the writer. It is safe
// for concurrent use.
type Hub struct {
	mu    sync.Mutex
	lines []OutputLine // ring; next is the slot the next line goes to
	next  int
	full  bool
	subs  map[*Subscription]struct{}
	now   func() time.Time
	// schema is the latest schema status, served to new subscribers.
	schema *SchemaStatus
}

// Subscription receives events added after it subscribed. Events that don't
// fit in its channel are dropped and counted.
type Subscription struct {
	// C delivers events. It is never closed; Unsubscribe when done.
	C       chan Event
	dropped int // guarded by the hub's mutex
}

// NewHub returns a hub keeping [DefaultMaxLines] lines.
func NewHub() *Hub { return NewHubSize(DefaultMaxLines) }

// NewHubSize returns a hub keeping the last n lines.
func NewHubSize(n int) *Hub {
	if n < 1 {
		n = 1
	}
	return &Hub{lines: make([]OutputLine, n), subs: map[*Subscription]struct{}{}, now: time.Now}
}

// Writer returns a writer that records what is written to it as lines of
// stream ("app" or "orb"). Partial lines wait for their newline.
func (h *Hub) Writer(stream string) io.Writer {
	return &lineWriter{hub: h, stream: stream}
}

// AddLine records one line of stream.
func (h *Hub) AddLine(stream, text string) {
	if len(text) > maxLineBytes {
		text = text[:maxLineBytes] + "…"
	}
	line := OutputLine{Time: h.now(), Stream: stream, Text: text}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines[h.next] = line
	h.next++
	if h.next == len(h.lines) {
		h.next, h.full = 0, true
	}
	h.publish(Event{Type: "output", Time: line.Time, Output: &line})
}

// SetState tells subscribers the app's status changed.
func (h *Hub) SetState(s AppStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publish(Event{Type: "state", Time: h.now(), State: &s})
}

// SetSchema keeps s as the latest schema status and tells subscribers.
func (h *Hub) SetSchema(s SchemaStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.schema = &s
	h.publish(Event{Type: "schema", Time: h.now(), Schema: &s})
}

// Schema returns the latest schema status, if one was set.
func (h *Hub) Schema() (SchemaStatus, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.schema == nil {
		return SchemaStatus{}, false
	}
	return *h.schema, true
}

// publish offers e to every subscriber. The caller holds the mutex.
func (h *Hub) publish(e Event) {
	for s := range h.subs {
		select {
		case s.C <- e:
		default:
			s.dropped++
		}
	}
}

// Lines returns the most recent lines, oldest first, at most limit (all
// when limit is 0 or more than the buffer holds).
func (h *Hub) Lines(limit int) []OutputLine {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []OutputLine
	if !h.full {
		out = append([]OutputLine(nil), h.lines[:h.next]...)
	} else {
		out = make([]OutputLine, 0, len(h.lines))
		out = append(out, h.lines[h.next:]...)
		out = append(out, h.lines[:h.next]...)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Capacity returns how many lines the hub keeps.
func (h *Hub) Capacity() int { return len(h.lines) }

// Subscribe returns a subscription to every event from now on.
func (h *Hub) Subscribe() *Subscription {
	s := &Subscription{C: make(chan Event, subscriberBuffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[s] = struct{}{}
	return s
}

// Unsubscribe stops sending events to s.
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, s)
}

// takeDropped returns how many events s missed since the last call.
func (h *Hub) takeDropped(s *Subscription) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := s.dropped
	s.dropped = 0
	return n
}

// lineWriter splits writes into lines for a hub.
type lineWriter struct {
	hub     *Hub
	stream  string
	mu      sync.Mutex
	partial []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rest := p
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		line := rest[:i]
		if len(w.partial) > 0 {
			line = append(w.partial, line...)
			w.partial = nil
		}
		w.hub.AddLine(w.stream, string(bytes.TrimRight(line, "\r")))
		rest = rest[i+1:]
	}
	if len(rest) > 0 {
		w.partial = append(w.partial, rest...)
		if len(w.partial) > maxLineBytes {
			w.hub.AddLine(w.stream, string(w.partial))
			w.partial = nil
		}
	}
	return len(p), nil
}
