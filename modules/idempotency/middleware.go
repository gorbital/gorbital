package idempotency

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/httpx"
)

// DefaultMaxResponseBytes is the largest response body stored without
// [WithMaxResponseBytes]. Larger responses reach the client but release the
// key.
const DefaultMaxResponseBytes = 1 << 20

// finishTimeout bounds storing or releasing a key after the handler
// returned, whatever happened to the request's context.
const finishTimeout = 5 * time.Second

// replayedHeaders are the response headers stored and replayed. Set-Cookie is
// never among them: responses setting cookies aren't stored at all.
var replayedHeaders = []string{"Content-Type", "Location", "ETag"}

// releasedStatuses are the client errors a caller can resolve without
// changing the request: signing in, getting a permission or a fresher
// session, or waiting. They release the key like server errors.
var releasedStatuses = []int{
	http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests,
}

type middlewareOptions struct {
	skip             func(*http.Request) bool
	scope            func(*http.Request) (string, bool)
	maxResponseBytes int
}

// A MiddlewareOption configures [Middleware].
type MiddlewareOption interface{ apply(*middlewareOptions) }

type middlewareOptionFunc func(*middlewareOptions)

func (f middlewareOptionFunc) apply(o *middlewareOptions) { f(o) }

// WithSkip ignores the header on requests for which skip returns true, such
// as sign-in endpoints, whose responses carry session tokens.
func WithSkip(skip func(*http.Request) bool) MiddlewareOption {
	return middlewareOptionFunc(func(o *middlewareOptions) { o.skip = skip })
}

// WithScope sets who owns a request's keys. scope returns false for callers
// whose header is ignored. Default: [ActorScope].
func WithScope(scope func(*http.Request) (string, bool)) MiddlewareOption {
	return middlewareOptionFunc(func(o *middlewareOptions) { o.scope = scope })
}

// WithMaxResponseBytes sets the largest response body stored. Default:
// [DefaultMaxResponseBytes].
func WithMaxResponseBytes(n int) MiddlewareOption {
	return middlewareOptionFunc(func(o *middlewareOptions) { o.maxResponseBytes = n })
}

// ActorScope owns keys by the actor in the request's context: a user or a
// service account, by kind and ID. Anonymous and system actors have no
// scope, so their requests ignore the header.
func ActorScope(r *http.Request) (string, bool) {
	a, ok := actor.From(r.Context())
	if !ok || a.ID == "" || (a.Kind != actor.KindUser && a.Kind != actor.KindService) {
		return "", false
	}
	return string(a.Kind) + ":" + a.ID, true
}

type dontStoreKey struct{}

// DontStore stops the response of the current request from being stored, for
// responses that must be shown only once, such as a new API key. The key is
// released, so a retry runs the request again. It does nothing outside a
// request with an idempotency key.
func DontStore(ctx context.Context) {
	if flag, ok := ctx.Value(dontStoreKey{}).(*bool); ok {
		*flag = true
	}
}

// Middleware applies idempotency keys to POST and PATCH requests carrying
// the [Header] (other methods are idempotent already, or read-only). Put it
// after authentication, so the caller is known, and after any request body
// limit: the body is read into memory to fingerprint it.
//
//   - A key must be 1 to [MaxKeyLength] visible ASCII characters (400
//     invalid_idempotency_key), sent once.
//   - The first request runs; its response is stored unless it must be
//     released (see the package documentation).
//   - A retry with the same method, target and body gets the stored status,
//     Content-Type, Location, ETag and body, with [ReplayedHeader] "true".
//   - A retry with another method, target or body: 422
//     idempotency_key_reused. While the first request runs: 409
//     idempotency_in_progress with Retry-After.
//   - When the store can't be reached: 503 unavailable, without running the
//     request.
func Middleware(store *Store, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	o := middlewareOptions{
		skip:             func(*http.Request) bool { return false },
		scope:            ActorScope,
		maxResponseBytes: DefaultMaxResponseBytes,
	}
	for _, opt := range opts {
		opt.apply(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keys := r.Header.Values(Header)
			if len(keys) == 0 || (r.Method != http.MethodPost && r.Method != http.MethodPatch) || o.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			scope, ok := o.scope(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if len(keys) != 1 || !validKey(keys[0]) {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusBadRequest, "invalid_idempotency_key",
					"send one Idempotency-Key header of 1 to 255 visible ASCII characters"))
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				// Too large or broken: the handler reports the same error.
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), errReader{err}))
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			store.serve(w, r, next, o, scope, keys[0], Fingerprint(r.Method, r.URL.RequestURI(), body))
		})
	}
}

// serve runs or replays a request whose key and fingerprint are known.
func (s *Store) serve(w http.ResponseWriter, r *http.Request, next http.Handler, o middlewareOptions, scope, key string, fingerprint []byte) {
	ctx := r.Context()
	lock, stored, err := s.Claim(ctx, scope, key, fingerprint)
	switch {
	case errors.Is(err, ErrKeyReused):
		s.count(ctx, "key_reused")
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUnprocessableEntity, "idempotency_key_reused",
			"this idempotency key was used for a different request; use a new key for a new request"))
		return
	case errors.Is(err, ErrInProgress):
		s.count(ctx, "in_progress")
		w.Header().Set("Retry-After", "1")
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusConflict, "idempotency_in_progress",
			"a request with this idempotency key is still in progress; retry later"))
		return
	case err != nil:
		s.count(ctx, "unavailable")
		s.logger.ErrorContext(ctx, "claim idempotency key", "err", err)
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "unavailable",
			"idempotency keys are temporarily unavailable; retry later"))
		return
	case stored != nil:
		s.count(ctx, "replayed")
		h := w.Header()
		for name, values := range stored.Header {
			h[http.CanonicalHeaderKey(name)] = values
		}
		h.Set(ReplayedHeader, "true")
		w.WriteHeader(stored.Status)
		_, _ = w.Write(stored.Body)
		return
	}

	rec := &recorder{ResponseWriter: w, max: o.maxResponseBytes}
	dontStore := new(bool)
	finished := false
	defer func() {
		// Also runs when the handler panics: the key is released before the
		// panic reaches the recovery middleware.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
		defer cancel()
		if !finished || !rec.storable() || *dontStore {
			s.count(ctx, "released")
			if err := lock.Release(fctx); err != nil {
				s.logger.WarnContext(ctx, "release idempotency key", "err", err)
			}
			return
		}
		s.count(ctx, "stored")
		if err := lock.Complete(fctx, rec.response()); err != nil {
			// The key stays locked until its TTL: a retry meanwhile gets 409
			// rather than running the request again.
			s.logger.WarnContext(ctx, "store idempotent response", "err", err)
		}
	}()
	next.ServeHTTP(rec, r.WithContext(context.WithValue(ctx, dontStoreKey{}, dontStore)))
	finished = true
}

// validKey reports whether key is 1 to MaxKeyLength visible ASCII characters.
func validKey(key string) bool {
	if key == "" || len(key) > MaxKeyLength {
		return false
	}
	for i := range len(key) {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

// recorder passes a response through while keeping what is needed to store
// it.
type recorder struct {
	http.ResponseWriter
	max      int
	status   int
	header   http.Header
	body     bytes.Buffer
	cookies  bool
	overflow bool
}

func (rec *recorder) WriteHeader(status int) {
	if rec.status == 0 && status >= 200 {
		rec.snapshot(status)
	}
	rec.ResponseWriter.WriteHeader(status)
}

// snapshot records the final status and the headers sent with it.
func (rec *recorder) snapshot(status int) {
	rec.status = status
	h := rec.ResponseWriter.Header()
	rec.cookies = len(h.Values("Set-Cookie")) > 0
	rec.header = http.Header{}
	for _, name := range replayedHeaders {
		if v := h.Values(name); len(v) > 0 {
			rec.header[http.CanonicalHeaderKey(name)] = slices.Clone(v)
		}
	}
}

func (rec *recorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.WriteHeader(http.StatusOK)
	}
	if !rec.overflow {
		if rec.body.Len()+len(b) > rec.max {
			rec.overflow = true
			rec.body = bytes.Buffer{}
		} else {
			rec.body.Write(b)
		}
	}
	return rec.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (rec *recorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// storable reports whether the response is a final outcome that may be
// replayed.
func (rec *recorder) storable() bool {
	if rec.status == 0 {
		rec.snapshot(http.StatusOK) // a handler that wrote nothing answers 200
	}
	return rec.status < 500 && !slices.Contains(releasedStatuses, rec.status) && !rec.cookies && !rec.overflow
}

func (rec *recorder) response() Response {
	return Response{Status: rec.status, Header: rec.header, Body: rec.body.Bytes()}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
