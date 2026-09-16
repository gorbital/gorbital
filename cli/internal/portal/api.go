package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
)

// Status is GET /_portal/api/status.
type Status struct {
	Portal  Info              `json:"portal"`
	Project Project           `json:"project"`
	App     AppStatus         `json:"app"`
	Links   map[string]string `json:"links"`
	// Generators lists the generators this orb offers.
	Generators []string `json:"generators"`
}

// Info describes the portal itself.
type Info struct {
	// Version is the orb version.
	Version string `json:"version"`
	// UI is "bundled" when a built UI is embedded, "placeholder" otherwise.
	UI        string    `json:"ui"`
	StartedAt time.Time `json:"started_at"`
	// Database reports whether the portal can reach the app's database
	// (the Table Editor and Schema pages).
	Database bool `json:"database"`
}

// OutputList is GET /_portal/api/output: the most recent lines, oldest
// first.
type OutputList struct {
	// Max is how many lines the portal keeps.
	Max   int          `json:"max"`
	Lines []OutputLine `json:"lines"`
}

// Accepted is the answer to a queued action.
type Accepted struct {
	Accepted bool      `json:"accepted"`
	App      AppStatus `json:"app"`
}

// StreamEnd is the data of a stream's final "end" event.
type StreamEnd struct {
	// Reason is "max_duration" or "shutdown".
	Reason string `json:"reason"`
}

// StreamDropped is the data of a "dropped" event: events the client was
// too slow to receive.
type StreamDropped struct {
	Count int `json:"count"`
}

// GeneratorRequest is the body of a plan or apply request.
type GeneratorRequest struct {
	// Input has the generator's fields, as its CLI flags are named with
	// underscores: {"name": "CleanupSessions", "schedule": "0 3 * * *"}.
	Input json.RawMessage `json:"input"`
	// AllowDirty applies with uncommitted changes in the git repository.
	AllowDirty bool `json:"allow_dirty,omitempty"`
}

// GeneratorResponse is the answer to a plan or apply request.
type GeneratorResponse struct {
	Plan genplan.Plan `json:"plan"`
	// Applied reports whether the files were written.
	Applied bool `json:"applied"`
}

// maxRequestBody bounds generator inputs.
const maxRequestBody = 1 << 20

// apiHandler routes the API. Every request passed the guard.
func (s *Server) apiHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+APIPrefix+"status", s.serveStatus)
	mux.HandleFunc("GET "+APIPrefix+"session", func(w http.ResponseWriter, _ *http.Request) {
		_ = writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET "+APIPrefix+"output", s.serveOutput)
	mux.HandleFunc("GET "+APIPrefix+"events", s.serveEvents)
	mux.HandleFunc("POST "+APIPrefix+"app/restart", s.appAction(s.cfg.Supervisor.Restart))
	mux.HandleFunc("POST "+APIPrefix+"app/stop", s.appAction(s.cfg.Supervisor.Stop))
	mux.HandleFunc("POST "+APIPrefix+"app/start", s.appAction(s.cfg.Supervisor.Start))
	mux.HandleFunc("POST "+APIPrefix+"app/migrate", s.appAction(s.cfg.Supervisor.Migrate))
	mux.HandleFunc("POST "+APIPrefix+"app/migrate-down", s.appAction(s.cfg.Supervisor.MigrateDown))
	mux.HandleFunc("POST "+APIPrefix+"app/migrate-redo", s.appAction(s.cfg.Supervisor.MigrateRedo))
	mux.HandleFunc("POST "+APIPrefix+"generators/{name}/plan", s.serveGenerator(false))
	mux.HandleFunc("POST "+APIPrefix+"generators/{name}/apply", s.serveGenerator(true))
	mux.HandleFunc(APIPrefix, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "not_found", "no portal endpoint "+r.Method+" "+r.URL.Path)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				s.cfg.Logf("orb: dev portal panic on %s: %v", r.URL.Path, v)
				writeProblem(w, http.StatusInternalServerError, "internal_error", "the Dev Portal failed; see orb dev's output")
			}
		}()
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) status() Status {
	names := make([]string, 0, len(s.cfg.Generators))
	for name := range s.cfg.Generators {
		names = append(names, name)
	}
	slices.Sort(names)
	ui := "placeholder"
	if s.bundled {
		ui = "bundled"
	}
	return Status{
		Portal:     Info{Version: s.cfg.Version, UI: ui, StartedAt: s.startedAt, Database: s.cfg.Database.Open != nil},
		Project:    s.cfg.Project,
		App:        s.cfg.Supervisor.Status(),
		Links:      s.cfg.Links,
		Generators: names,
	}
}

func (s *Server) serveStatus(w http.ResponseWriter, _ *http.Request) {
	_ = writeJSON(w, http.StatusOK, s.status())
}

func (s *Server) serveOutput(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeProblem(w, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative number")
			return
		}
		limit = n
	}
	_ = writeJSON(w, http.StatusOK, OutputList{Max: s.cfg.Hub.Capacity(), Lines: s.cfg.Hub.Lines(limit)})
}

// appAction runs a supervisor action and answers 202 with the status as it
// is right after; the outcome arrives as state events.
func (s *Server) appAction(action func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := action(); err != nil {
			writeProblem(w, http.StatusConflict, "app_action_failed", err.Error())
			return
		}
		_ = writeJSON(w, http.StatusAccepted, Accepted{Accepted: true, App: s.cfg.Supervisor.Status()})
	}
}

// serveEvents streams state changes and output lines as Server-Sent Events.
// The first event is the current state, so a client that connects late sees
// where things stand; fetch /output afterwards to fill the backlog.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
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
	sub := s.cfg.Hub.Subscribe()
	defer s.cfg.Hub.Unsubscribe(sub)
	if err := write("state", s.cfg.Supervisor.Status()); err != nil {
		return
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
			_ = write("end", StreamEnd{Reason: "max_duration"})
			return
		case <-keepAlive.C:
			_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case e := <-sub.C:
			if n := s.cfg.Hub.takeDropped(sub); n > 0 {
				if err := write("dropped", StreamDropped{Count: n}); err != nil {
					return
				}
			}
			if err := write(e.Type, e); err != nil {
				return
			}
		}
	}
}

// serveGenerator plans, or plans and applies, the named generator.
func (s *Server) serveGenerator(apply bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		g, ok := s.cfg.Generators[name]
		if !ok {
			writeProblem(w, http.StatusNotFound, "generator_not_found", "no generator "+name+"; this orb has: "+strings.Join(s.status().Generators, ", "))
			return
		}
		var req GeneratorRequest
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
		if err != nil {
			writeProblem(w, http.StatusRequestEntityTooLarge, "body_too_large", "the request body is too large")
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeProblem(w, http.StatusBadRequest, "invalid_json", "the body must be JSON: "+err.Error())
				return
			}
		}
		if len(req.Input) == 0 {
			req.Input = json.RawMessage("{}")
		}
		var plan genplan.Plan
		if apply {
			plan, err = g.Apply(r.Context(), req.Input, req.AllowDirty)
		} else {
			plan, err = g.Plan(r.Context(), req.Input)
		}
		if err != nil {
			status, code := http.StatusUnprocessableEntity, "generator_failed"
			switch {
			case errors.Is(err, genplan.ErrExists), errors.Is(err, genplan.ErrStale):
				status, code = http.StatusConflict, "plan_conflict"
			}
			writeProblem(w, status, code, err.Error())
			return
		}
		_ = writeJSON(w, http.StatusOK, GeneratorResponse{Plan: plan, Applied: apply})
	}
}
