package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultFlushInterval is how often a collector writes its windows
	// without [WithFlushInterval].
	DefaultFlushInterval = 15 * time.Second
	// DefaultMaxSeries is how many method and route pairs one minute keeps
	// without [WithMaxSeries].
	DefaultMaxSeries = 500
	// DefaultMaxPendingMinutes is how many unwritten minutes a collector
	// keeps while its sink fails, without [WithMaxPendingMinutes].
	DefaultMaxPendingMinutes = 10

	// OverflowRoute is the route of requests counted after a minute already
	// has its maximum number of series.
	OverflowRoute = "_overflow"
	// OtherMethod replaces request methods that aren't standard HTTP
	// methods, as OpenTelemetry does.
	OtherMethod = "_OTHER"

	// closeGrace is how long a minute stays in memory after the next one
	// started before its final write, so requests that read the old window
	// just before it closed are included.
	closeGrace = time.Second
	// finalFlushTimeout bounds the write when Run stops.
	finalFlushTimeout = 5 * time.Second
)

// Request is one finished HTTP request, as [Collector.Middleware] recorded
// it.
type Request struct {
	// Time is when the request finished.
	Time time.Time
	// Method is the request method: a standard method, or [OtherMethod].
	Method string
	// Route is the path of the pattern the router matched, such as
	// "/v1/projects/{id}"; empty when no route matched, such as a request
	// answered by middleware.
	Route    string
	Status   int
	Duration time.Duration

	// Path (without the query), RequestID and TraceID are set only for
	// subscribers ([Collector.Subscribe]). The collector never stores or
	// aggregates them.
	Path      string
	RequestID string
	TraceID   string
}

// A Sink stores a collector's minutes. [Store] implements it.
type Sink interface {
	WriteMinutes(ctx context.Context, minutes []Minute) error
}

// Minute is one instance's requests for one method and route during one
// minute. Written again while the minute is in progress, it carries the
// totals so far.
type Minute struct {
	// Start is the minute's first instant, in UTC.
	Start    time.Time
	Instance string
	Method   string
	Route    string
	Stats
}

// Collector counts an instance's HTTP requests in one-minute windows, per
// method and route, and writes them to its [Sink] periodically when run.
// Its memory is bounded: at most [WithMaxSeries] series per minute, each a
// fixed-size histogram, for at most [WithMaxPendingMinutes] minutes. It is
// safe for concurrent use.
type Collector struct {
	instance      string
	sink          Sink
	logger        *slog.Logger
	now           func() time.Time
	flushInterval time.Duration
	maxSeries     int
	maxPending    int

	mu      sync.Mutex // guards rotation, pending and subscribers
	current atomic.Pointer[window]
	pending []*window // closed minutes not yet written for the last time
	latest  int64     // the latest minute a window was started for

	subscribers atomic.Pointer[[]*subscriber]
	lost        atomic.Int64
}

// An Option configures a [Collector].
type Option func(*Collector)

// WithInstance names the instance in the stored minutes, such as the
// release tracker's instance ID. Default: a random ID.
func WithInstance(id string) Option { return func(c *Collector) { c.instance = id } }

// WithSink sets where [Collector.Run] writes minutes. Without a sink, Run
// only discards finished minutes.
func WithSink(sink Sink) Option { return func(c *Collector) { c.sink = sink } }

// WithFlushInterval sets how often Run writes minutes: 1 second to 1
// minute. Default: [DefaultFlushInterval].
func WithFlushInterval(d time.Duration) Option {
	return func(c *Collector) { c.flushInterval = d }
}

// WithMaxSeries sets how many method and route pairs one minute keeps;
// later pairs are counted under [OverflowRoute]. Default:
// [DefaultMaxSeries].
func WithMaxSeries(n int) Option { return func(c *Collector) { c.maxSeries = n } }

// WithMaxPendingMinutes sets how many finished minutes wait for a failing
// sink before the oldest is dropped. Default: [DefaultMaxPendingMinutes].
func WithMaxPendingMinutes(n int) Option { return func(c *Collector) { c.maxPending = n } }

// WithLogger sets the logger for failed writes. Default: discard.
func WithLogger(logger *slog.Logger) Option { return func(c *Collector) { c.logger = logger } }

// WithClock uses now instead of time.Now to place requests in minutes, for
// tests.
func WithClock(now func() time.Time) Option { return func(c *Collector) { c.now = now } }

// NewCollector returns a collector.
func NewCollector(opts ...Option) (*Collector, error) {
	c := &Collector{
		logger:        slog.New(slog.DiscardHandler),
		now:           time.Now,
		flushInterval: DefaultFlushInterval,
		maxSeries:     DefaultMaxSeries,
		maxPending:    DefaultMaxPendingMinutes,
	}
	for _, o := range opts {
		o(c)
	}
	var errs []error
	if c.instance == "" {
		b := make([]byte, 8)
		_, _ = rand.Read(b) // never fails (crypto/rand)
		c.instance = hex.EncodeToString(b)
	}
	if len(c.instance) > MaxInstanceLength {
		errs = append(errs, errors.New("instance ID must be at most 64 bytes"))
	}
	if c.flushInterval < time.Second || c.flushInterval > time.Minute {
		errs = append(errs, errors.New("flush interval must be between 1 second and 1 minute"))
	}
	if c.maxSeries < 1 || c.maxPending < 1 {
		errs = append(errs, errors.New("max series and pending minutes must be at least 1"))
	}
	if c.logger == nil || c.now == nil {
		errs = append(errs, errors.New("logger and clock are required"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, errors.Join(errors.New("observability: invalid collector"), err)
	}
	return c, nil
}

// MaxInstanceLength is the longest instance ID stored.
const MaxInstanceLength = 64

// Instance returns the instance ID the collector writes minutes under.
func (c *Collector) Instance() string { return c.instance }

// Record counts a finished request. [Collector.Middleware] calls it; call it
// directly only for requests served another way.
func (c *Collector) Record(r Request) {
	minute := r.Time.Truncate(time.Minute).Unix()
	w := c.current.Load()
	if w == nil || w.start != minute {
		if w = c.windowFor(minute, r.Time); w == nil {
			c.lost.Add(1)
			return
		}
	}
	w.record(normalizeMethod(r.Method), r.Route, r.Status, r.Duration, c.maxSeries)

	if subs := c.subscribers.Load(); subs != nil {
		for _, s := range *subs {
			s.fn(r)
		}
	}
}

// windowFor returns the window for a request that finished in minute,
// starting a new current window when minute is later than every window so
// far. A request finishing in an earlier minute (the clock moved back)
// counts in the minute it names when that is still held, else in the
// latest minute; it is dropped when neither is held, because writing a new
// window for a minute already written would replace its stored totals.
func (c *Collector) windowFor(minute int64, now time.Time) *window {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w := c.heldLocked(minute); w != nil {
		return w
	}
	if c.latest != 0 && minute <= c.latest {
		return c.heldLocked(c.latest)
	}
	if cur := c.current.Load(); cur != nil {
		c.closeLocked(cur, now)
	}
	w := newWindow(minute)
	c.latest = minute
	c.current.Store(w)
	return w
}

// heldLocked returns the current or pending window of minute, or nil. c.mu
// must be held.
func (c *Collector) heldLocked(minute int64) *window {
	if cur := c.current.Load(); cur != nil && cur.start == minute {
		return cur
	}
	for _, w := range c.pending {
		if w.start == minute {
			return w
		}
	}
	return nil
}

// closeLocked moves w to the pending minutes, dropping the oldest when there
// are too many. c.mu must be held.
func (c *Collector) closeLocked(w *window, now time.Time) {
	w.closedAt = now
	c.pending = append(c.pending, w)
	for len(c.pending) > c.maxPending {
		c.lost.Add(c.pending[0].requests())
		c.pending = slices.Delete(c.pending, 0, 1)
	}
}

// Lost returns how many requests were dropped without being written because
// the sink failed for longer than the pending minutes last.
func (c *Collector) Lost() int64 { return c.lost.Load() }

// Minutes returns what the collector holds now: the current minute and
// finished minutes not yet written for the last time, oldest first.
func (c *Collector) Minutes() []Minute {
	c.mu.Lock()
	windows := slices.Clone(c.pending)
	if cur := c.current.Load(); cur != nil {
		windows = append(windows, cur)
	}
	c.mu.Unlock()
	var out []Minute
	for _, w := range windows {
		out = append(out, w.minutes(c.instance)...)
	}
	return out
}

// Flush writes every minute the collector holds to the sink, then forgets
// minutes that ended before the write started. The current minute is
// written again at the next flush, with its new totals.
func (c *Collector) Flush(ctx context.Context) error {
	now := c.now()
	c.mu.Lock()
	if cur := c.current.Load(); cur != nil && cur.start < now.Truncate(time.Minute).Unix() {
		c.current.Store(nil)
		c.closeLocked(cur, now)
	}
	windows := slices.Clone(c.pending)
	if cur := c.current.Load(); cur != nil {
		windows = append(windows, cur)
	}
	c.mu.Unlock()

	var minutes []Minute
	for _, w := range windows {
		minutes = append(minutes, w.minutes(c.instance)...)
	}
	if len(minutes) > 0 && c.sink != nil {
		if err := c.sink.WriteMinutes(ctx, minutes); err != nil {
			return err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = slices.DeleteFunc(c.pending, func(w *window) bool {
		return slices.Contains(windows, w) && !w.closedAt.After(now.Add(-closeGrace))
	})
	return nil
}

// Run writes minutes to the sink every flush interval until ctx ends, then
// writes once more. Failed writes are logged and retried at the next
// interval; Run always returns nil, so observability never stops the app.
func (c *Collector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// The last minutes, including a partial one: a clean shutdown
			// loses nothing.
			c.closeCurrent()
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalFlushTimeout)
			c.flushAndLog(flushCtx)
			cancel()
			return nil
		case <-ticker.C:
			flushCtx, cancel := context.WithTimeout(ctx, c.flushInterval)
			c.flushAndLog(flushCtx)
			cancel()
		}
	}
}

// closeCurrent closes the current window so the final flush forgets it.
func (c *Collector) closeCurrent() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur := c.current.Load(); cur != nil {
		c.current.Store(nil)
		c.closeLocked(cur, c.now().Add(-closeGrace))
	}
}

func (c *Collector) flushAndLog(ctx context.Context) {
	if err := c.Flush(ctx); err != nil {
		c.logger.WarnContext(ctx, "write request metrics", "instance_id", c.instance, "err", err)
	}
}

type subscriber struct{ fn func(Request) }

// Subscribe calls fn with every request recorded from now on, until
// unsubscribe is called. fn runs on the request's goroutine before the
// response is complete, so it must return quickly and never block, such as
// by appending to a ring buffer. Requests carry their path, request ID and
// trace ID only for subscribers.
func (c *Collector) Subscribe(fn func(Request)) (unsubscribe func()) {
	s := &subscriber{fn: fn}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers.Store(appendSubscriber(c.subscribers.Load(), s))
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			old := c.subscribers.Load()
			if old == nil {
				return
			}
			next := slices.DeleteFunc(slices.Clone(*old), func(o *subscriber) bool { return o == s })
			if len(next) == 0 {
				c.subscribers.Store(nil)
				return
			}
			c.subscribers.Store(&next)
		})
	}
}

func appendSubscriber(old *[]*subscriber, s *subscriber) *[]*subscriber {
	var next []*subscriber
	if old != nil {
		next = slices.Clone(*old)
	}
	next = append(next, s)
	return &next
}

// hasSubscribers reports whether any subscriber wants requests' paths and
// IDs.
func (c *Collector) hasSubscribers() bool { return c.subscribers.Load() != nil }

// standardMethods are the methods kept as they are; others become
// OtherMethod, so clients can't create series.
var standardMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
}

func normalizeMethod(m string) string {
	for _, s := range standardMethods {
		if m == s {
			return s
		}
	}
	return OtherMethod
}

// routeOf returns the path of a ServeMux pattern: "GET /v1/items/{id}"
// becomes "/v1/items/{id}", and a pattern with a host keeps only its path.
func routeOf(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return ""
}

// window holds one minute's series.
type window struct {
	start    int64 // Unix seconds of the minute's start
	closedAt time.Time

	mu     sync.RWMutex
	series map[seriesKey]*series
}

type seriesKey struct{ method, route string }

// series counts one method and route. Counters are updated without locks,
// so a snapshot taken while requests finish can be a few requests apart
// between fields; the next write of the minute catches up.
type series struct {
	requests, clientErrors, serverErrors atomic.Int64
	durationSum, durationMax             atomic.Int64
	buckets                              [BucketCount]atomic.Int64
}

func newWindow(start int64) *window {
	return &window{start: start, series: map[seriesKey]*series{}}
}

func (w *window) record(method, route string, status int, d time.Duration, maxSeries int) {
	key := seriesKey{method, route}
	w.mu.RLock()
	s := w.series[key]
	w.mu.RUnlock()
	if s == nil {
		w.mu.Lock()
		if s = w.series[key]; s == nil {
			if len(w.series) >= maxSeries {
				key = seriesKey{method: OtherMethod, route: OverflowRoute}
				s = w.series[key]
			}
			if s == nil {
				s = &series{}
				w.series[key] = s
			}
		}
		w.mu.Unlock()
	}

	d = max(d, 0)
	s.requests.Add(1)
	switch {
	case status >= 500:
		s.serverErrors.Add(1)
	case status >= 400:
		s.clientErrors.Add(1)
	}
	s.durationSum.Add(int64(d))
	for {
		cur := s.durationMax.Load()
		if int64(d) <= cur || s.durationMax.CompareAndSwap(cur, int64(d)) {
			break
		}
	}
	s.buckets[bucketOf(d)].Add(1)
}

func (w *window) minutes(instance string) []Minute {
	w.mu.RLock()
	defer w.mu.RUnlock()
	start := time.Unix(w.start, 0).UTC()
	out := make([]Minute, 0, len(w.series))
	for key, s := range w.series {
		m := Minute{Start: start, Instance: instance, Method: key.method, Route: key.route, Stats: Stats{
			Requests:     s.requests.Load(),
			ClientErrors: s.clientErrors.Load(),
			ServerErrors: s.serverErrors.Load(),
			DurationSum:  time.Duration(s.durationSum.Load()),
			DurationMax:  time.Duration(s.durationMax.Load()),
			Buckets:      make([]int64, BucketCount),
		}}
		for i := range s.buckets {
			m.Buckets[i] = s.buckets[i].Load()
		}
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b Minute) int {
		return strings.Compare(a.Route+" "+a.Method, b.Route+" "+b.Method)
	})
	return out
}

func (w *window) requests() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var n int64
	for _, s := range w.series {
		n += s.requests.Load()
	}
	return n
}
