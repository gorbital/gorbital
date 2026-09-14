// Package ratelimit provides in-memory token-bucket rate limiting keyed by a
// string (an IP address, account or API key) and HTTP middleware.
//
// Limits are per process. Behind several instances each instance enforces
// its own limit; a shared store can implement the same behaviour later.
//
// Stability: pre-1.0 (ADR-0015).
package ratelimit

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Defaults for [New].
const (
	DefaultIdleTTL = 10 * time.Minute
	DefaultMaxKeys = 100_000
)

// A Limiter tracks one token bucket per key. It is safe for concurrent use.
type Limiter struct {
	limit   rate.Limit
	burst   int
	idleTTL time.Duration
	maxKeys int
	now     func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// An Option configures a [Limiter].
type Option interface{ apply(*Limiter) }

type idleTTLOption time.Duration

func (o idleTTLOption) apply(l *Limiter) { l.idleTTL = time.Duration(o) }

// WithIdleTTL sets how long an unused key is remembered. Default: [DefaultIdleTTL].
func WithIdleTTL(d time.Duration) Option { return idleTTLOption(d) }

type maxKeysOption int

func (o maxKeysOption) apply(l *Limiter) { l.maxKeys = int(o) }

// WithMaxKeys bounds memory use. When the limit is reached and no idle keys
// can be evicted, new keys are allowed without tracking (fail open), so a
// flood of distinct keys can't lock out all clients. Default: [DefaultMaxKeys].
func WithMaxKeys(n int) Option { return maxKeysOption(n) }

type clockOption func() time.Time

func (o clockOption) apply(l *Limiter) { l.now = o }

// WithClock sets the time source, for tests.
func WithClock(now func() time.Time) Option { return clockOption(now) }

// New returns a limiter allowing perSecond requests per key on average, with
// bursts of up to burst requests.
func New(perSecond float64, burst int, opts ...Option) *Limiter {
	l := &Limiter{
		limit:   rate.Limit(perSecond),
		burst:   burst,
		idleTTL: DefaultIdleTTL,
		maxKeys: DefaultMaxKeys,
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
	for _, o := range opts {
		o.apply(l)
	}
	return l
}

// Allow reports whether a request for key may proceed. When it may not,
// retryAfter is how long until one token is available.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, found := l.buckets[key]
	if !found {
		if len(l.buckets) >= l.maxKeys {
			l.evictIdle(now)
			if len(l.buckets) >= l.maxKeys {
				return true, 0
			}
		}
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	r := b.limiter.ReserveN(now, 1)
	if !r.OK() {
		return false, time.Duration(1<<63 - 1)
	}
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// evictIdle removes keys unused for longer than idleTTL. l.mu must be held.
func (l *Limiter) evictIdle(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > l.idleTTL {
			delete(l.buckets, k)
		}
	}
}

// KeyFunc extracts the rate-limit key from a request. An empty key skips
// limiting for that request.
type KeyFunc func(r *http.Request) string

// ByRemoteIP keys requests by the connection's remote IP. Behind a proxy,
// run trusted-proxy middleware first so RemoteAddr holds the client address.
func ByRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware limits requests with l. Limited requests receive onLimit, or a
// 429 application/problem+json response with a Retry-After header when
// onLimit is nil.
func Middleware(l *Limiter, key KeyFunc, onLimit http.Handler) func(http.Handler) http.Handler {
	if onLimit == nil {
		onLimit = http.HandlerFunc(tooManyRequests)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			ok, retry := l.Allow(k)
			if !ok {
				secs := int(retry.Round(time.Second) / time.Second)
				w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
				onLimit.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func tooManyRequests(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"title":"Too Many Requests","status":429,"code":"rate_limited","detail":"too many requests, retry later"}`))
}
