package devconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"gorbital.dev/httpx"
)

// Index lists the console's endpoints: GET /_dev/.
type Index struct {
	// Endpoints are the paths this app serves, sorted.
	Endpoints []string `json:"endpoints"`
}

// RequestList is the most recent requests, newest first: GET
// /_dev/requests.
type RequestList struct {
	// Max is how many requests the console keeps.
	Max      int       `json:"max"`
	Requests []Request `json:"requests"`
}

// LogList is the most recent log records, newest first: GET /_dev/logs.
type LogList struct {
	// Max is how many records the console keeps.
	Max  int   `json:"max"`
	Logs []Log `json:"logs"`
}

// ConfigList is the environment variables the app read: GET /_dev/config.
type ConfigList struct {
	Variables []EnvKey `json:"variables"`
}

// RouteList is the app's routes: GET /_dev/routes.
type RouteList struct {
	Routes []Route `json:"routes"`
}

// JobRunList is the most recent job runs, newest first: GET /_dev/jobs.
type JobRunList struct {
	Runs []JobRun `json:"runs"`
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

// endpoints returns the console's paths; those without a source aren't
// present.
func (c *Console) endpoints() map[string]endpoint {
	s := c.sources
	list := []endpoint{
		{path: Prefix + "openapi.json", present: true, serve: func(w http.ResponseWriter, _ *http.Request) error {
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write(OpenAPI())
			return err
		}},
		{path: Prefix + "requests", present: true, serve: func(w http.ResponseWriter, _ *http.Request) error {
			return writeJSON(w, RequestList{Max: c.requests.capacity(), Requests: newestFirst(c.requests.list())})
		}},
		{path: Prefix + "requests/stream", present: true, serve: func(w http.ResponseWriter, r *http.Request) error {
			return serveStream(c, w, r, c.requests, "request")
		}},
		{path: Prefix + "logs", present: c.logs != nil, serve: func(w http.ResponseWriter, _ *http.Request) error {
			return writeJSON(w, LogList{Max: c.logs.buf.capacity(), Logs: newestFirst(c.logs.List())})
		}},
		{path: Prefix + "logs/stream", present: c.logs != nil, serve: func(w http.ResponseWriter, r *http.Request) error {
			return serveStream(c, w, r, c.logs.buf, "log")
		}},
		section(Prefix+"app", s.App, func(v App) any { return v }),
		section(Prefix+"routes", s.Routes, func(v []Route) any { return RouteList{Routes: nonNil(v)} }),
		section(Prefix+"config", s.Config, func(v []EnvKey) any { return ConfigList{Variables: nonNil(v)} }),
		section(Prefix+"mail", s.Mail, func(v Mail) any { return v }),
		section(Prefix+"migrations", s.Migrations, func(v Migrations) any { return v }),
		section(Prefix+"jobs", s.Jobs, func(v []JobRun) any { return JobRunList{Runs: nonNil(v)} }),
	}
	index := Index{Endpoints: []string{Prefix}}
	out := map[string]endpoint{}
	for _, e := range list {
		out[e.path] = e
		if e.present {
			index.Endpoints = append(index.Endpoints, e.path)
		}
	}
	slices.Sort(index.Endpoints)
	out[Prefix] = endpoint{path: Prefix, present: true, serve: func(w http.ResponseWriter, _ *http.Request) error {
		return writeJSON(w, index)
	}}
	return out
}

// section serves an app-specific source's value, wrapped by wrap.
func section[T any](path string, source func(context.Context) (T, error), wrap func(T) any) endpoint {
	return endpoint{path: path, present: source != nil, serve: func(w http.ResponseWriter, r *http.Request) error {
		v, err := source(r.Context())
		if err != nil {
			return err
		}
		return writeJSON(w, wrap(v))
	}}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func newestFirst[T any](s []T) []T {
	slices.Reverse(s)
	return s
}

// openStream counts a new stream, or reports why it can't start.
func (c *Console) openStream() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.closed:
		return ErrStreamsClosed
	case c.streams >= c.maxStreams:
		return errTooManyStreams
	}
	c.streams++
	return nil
}

func (c *Console) closeStream() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streams--
}

var errTooManyStreams = errors.New("devconsole: too many streams")

// serveStream sends buf's new items as Server-Sent Events named event until
// the client goes away, the stream reaches its maximum duration or the
// console closes.
func serveStream[T any](c *Console, w http.ResponseWriter, r *http.Request, buf *buffer[T], event string) error {
	switch err := c.openStream(); {
	case errors.Is(err, errTooManyStreams):
		w.Header().Set("Retry-After", "5")
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusTooManyRequests, "rate_limited",
			"the dev console serves "+strconv.Itoa(c.maxStreams)+" streams at once; close another one"))
		return nil
	case err != nil:
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "unavailable", "the app is shutting down"))
		return nil
	}
	defer c.closeStream()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return nil
	}

	sub := buf.subscribe(streamBuffer)
	defer buf.unsubscribe(sub)

	rc := http.NewResponseController(w)
	write := func(text string) error {
		_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := w.Write([]byte(text)); err != nil {
			return err
		}
		return rc.Flush()
	}
	send := func(name string, data any) error {
		payload, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return write("event: " + name + "\ndata: " + string(payload) + "\n\n")
	}
	sendDropped := func() error {
		if n := buf.takeDropped(sub); n > 0 {
			return send("dropped", StreamDropped{Count: n})
		}
		return nil
	}

	if write("retry: 3000\n\n") != nil {
		return nil
	}
	expired := time.NewTimer(c.streamDuration)
	defer expired.Stop()
	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return nil
		case <-c.close:
			_ = send("end", StreamEnd{Reason: "shutdown"})
			return nil
		case <-expired.C:
			_ = send("end", StreamEnd{Reason: "max_duration"})
			return nil
		case <-keepAlive.C:
			if err = sendDropped(); err == nil {
				err = write(": keep-alive\n\n")
			}
		case v := <-sub.ch:
			if err = sendDropped(); err == nil {
				err = send(event, v)
			}
		}
		if err != nil {
			return nil // the client is gone; nothing to report
		}
	}
}
