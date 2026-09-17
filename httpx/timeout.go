package httpx

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gorbital.dev/requestid"
)

// Timeout gives each request a context deadline d from now. When the
// deadline passes before the handler has started its response, the client
// gets a 503 problem with code "request_timeout", and whatever the handler
// writes afterwards is discarded. Late writes report success, so frameworks
// that treat a failed write as a bug (Huma panics) don't turn every timeout
// into a logged panic; the handler learns of the timeout from its cancelled
// context. Flushing, hijacking and deadline changes after a timeout return
// [http.ErrHandlerTimeout].
// A d of zero or less turns it off.
//
// Once the handler has started its response (written the status or body,
// flushed, or hijacked the connection), the deadline only cancels the
// context: nothing is buffered, so streaming and [http.ResponseController]
// work as without the middleware. Streams meant to outlive d, such as
// server-sent events, belong on routes without this middleware.
//
// The handler runs on the request's goroutine, so the request ends when the
// handler returns: pass r.Context() to everything that waits, so it stops
// at the deadline. The 503 is written and flushed at the deadline with a
// Content-Length, so an HTTP/1.1 client has the whole response even while
// a handler that ignores its context keeps the connection. Install it after
// [Recover] and [RequestID].
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			r = r.WithContext(ctx)

			tw := &timeoutWriter{w: w, r: r, h: w.Header().Clone()}
			tw.running.Add(1)
			timer := time.AfterFunc(d, func() {
				defer tw.running.Done()
				tw.timeOut()
			})
			returned := false
			defer func() {
				if timer.Stop() {
					tw.running.Done()
				}
				tw.running.Wait()
				tw.finish(returned)
			}()
			next.ServeHTTP(tw, r)
			returned = true
		})
	}
}

// timeoutWriter serialises the handler's writes with the timeout response,
// which is written from another goroutine at the deadline. Until the
// response starts, the handler's headers live in h, so the 503 can use w's
// header map without racing with the handler.
type timeoutWriter struct {
	w       http.ResponseWriter
	r       *http.Request
	running sync.WaitGroup // the timeout callback, while it may run

	mu       sync.Mutex
	h        http.Header
	started  bool // the handler's status was sent, flushed or hijacked
	timedOut bool // the deadline passed first: the handler's writes are discarded
	answered bool // the 503 was written
}

// timeOut answers 503, unless the handler's response has started.
func (tw *timeoutWriter) timeOut() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.started {
		return
	}
	tw.timedOut = true
	tw.answerLocked()
}

// finish runs when the handler has returned or panicked and the timeout
// callback can no longer run. A handler that returned without responding
// after the deadline gets the 503; one that returned in time gets its
// headers copied, which net/http sends with an implicit 200. After a panic
// it leaves the response to [Recover].
func (tw *timeoutWriter) finish(returned bool) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	switch {
	case tw.started || tw.answered:
	case tw.refuseLocked():
		if returned {
			tw.answerLocked()
		}
	default:
		tw.copyHeaders()
	}
}

// answerLocked writes the 503 problem once, with a Content-Length, and
// flushes it.
func (tw *timeoutWriter) answerLocked() {
	if tw.answered {
		return
	}
	tw.answered = true
	p := NewProblem(http.StatusServiceUnavailable, "request_timeout", "the request took too long; try again later")
	p.RequestID = requestid.From(tw.r.Context())
	body, err := json.Marshal(p)
	if err != nil {
		return
	}
	body = append(body, '\n')
	h := tw.w.Header()
	h.Set("Content-Type", ProblemContentType)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	tw.w.WriteHeader(p.Status)
	_, _ = tw.w.Write(body)
	_ = http.NewResponseController(tw.w).Flush()
}

// refuseLocked reports whether the handler may no longer write: the
// deadline passed before its response started. Checking the context here,
// not only the callback's flag, makes a handler that reacts to the deadline
// lose to the timeout whichever goroutine runs first.
func (tw *timeoutWriter) refuseLocked() bool {
	if !tw.timedOut && !tw.started && errors.Is(tw.r.Context().Err(), context.DeadlineExceeded) {
		tw.timedOut = true
	}
	return tw.timedOut
}

func (tw *timeoutWriter) copyHeaders() {
	dst := tw.w.Header()
	clear(dst)
	maps.Copy(dst, tw.h)
}

// startLocked sends the handler's headers and marks the response started.
func (tw *timeoutWriter) startLocked() {
	tw.copyHeaders()
	tw.started = true
}

// Header returns the handler's header map: its own copy until the response
// starts, then the real one, so trailers set after writing are sent.
func (tw *timeoutWriter) Header() http.Header {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.started {
		return tw.w.Header()
	}
	return tw.h
}

// WriteHeader sends the status and headers, unless the request timed out.
func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	switch {
	case tw.started || tw.refuseLocked():
		return
	case code >= 100 && code < 200 && code != http.StatusSwitchingProtocols:
		// Informational responses (103 Early Hints) don't start the response.
		tw.copyHeaders()
		tw.w.WriteHeader(code)
		return
	}
	tw.startLocked()
	tw.w.WriteHeader(code)
}

// Write writes the body. After a timeout it discards b and reports
// success (see [Timeout]).
func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started {
		if tw.refuseLocked() {
			return len(b), nil
		}
		tw.startLocked()
	}
	return tw.w.Write(b)
}

// FlushError flushes the response, starting it; see [http.ResponseController].
func (tw *timeoutWriter) FlushError() error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started {
		if tw.refuseLocked() {
			return http.ErrHandlerTimeout
		}
		tw.startLocked()
	}
	return http.NewResponseController(tw.w).Flush()
}

// Flush implements [http.Flusher].
func (tw *timeoutWriter) Flush() { _ = tw.FlushError() }

// Hijack takes over the connection; see [http.ResponseController].
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started && tw.refuseLocked() {
		return nil, nil, http.ErrHandlerTimeout
	}
	conn, rw, err := http.NewResponseController(tw.w).Hijack()
	if err == nil {
		tw.started = true
	}
	return conn, rw, err
}

// SetReadDeadline sets the read deadline; see [http.ResponseController].
func (tw *timeoutWriter) SetReadDeadline(deadline time.Time) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started && tw.refuseLocked() {
		return http.ErrHandlerTimeout
	}
	return http.NewResponseController(tw.w).SetReadDeadline(deadline)
}

// SetWriteDeadline sets the write deadline; see [http.ResponseController].
func (tw *timeoutWriter) SetWriteDeadline(deadline time.Time) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started && tw.refuseLocked() {
		return http.ErrHandlerTimeout
	}
	return http.NewResponseController(tw.w).SetWriteDeadline(deadline)
}

// EnableFullDuplex lets the handler read the body while writing; see
// [http.ResponseController].
func (tw *timeoutWriter) EnableFullDuplex() error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started && tw.refuseLocked() {
		return http.ErrHandlerTimeout
	}
	return http.NewResponseController(tw.w).EnableFullDuplex()
}

// Unwrap returns the underlying writer, for interfaces this writer doesn't
// forward. After a timeout it returns a writer that discards everything:
// the underlying one carries the 503.
func (tw *timeoutWriter) Unwrap() http.ResponseWriter {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.started && tw.refuseLocked() {
		return discardWriter{}
	}
	return tw.w
}

// discardWriter is what a timed-out handler's unwrapped writer is.
type discardWriter struct{}

func (discardWriter) Header() http.Header         { return http.Header{} }
func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardWriter) WriteHeader(int)             {}
