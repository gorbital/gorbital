package observability_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorbital.dev/modules/observability"
)

// clock is a settable test clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 9, 16, 12, 0, 10, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// memorySink keeps the last write of each minute, like the table.
type memorySink struct {
	mu     sync.Mutex
	rows   map[string]observability.Minute
	writes int
	fail   error
}

func (s *memorySink) WriteMinutes(_ context.Context, minutes []observability.Minute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	if s.rows == nil {
		s.rows = map[string]observability.Minute{}
	}
	s.writes++
	for _, m := range minutes {
		s.rows[fmt.Sprint(m.Start.Unix(), m.Instance, m.Method, m.Route)] = m
	}
	return nil
}

func (s *memorySink) total() observability.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var t observability.Stats
	for _, m := range s.rows {
		t.Requests += m.Requests
		t.ServerErrors += m.ServerErrors
		t.ClientErrors += m.ClientErrors
	}
	return t
}

func newCollector(t testing.TB, opts ...observability.Option) *observability.Collector {
	t.Helper()
	c, err := observability.NewCollector(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func find(minutes []observability.Minute, method, route string) (observability.Minute, bool) {
	for _, m := range minutes {
		if m.Method == method && m.Route == route {
			return m, true
		}
	}
	return observability.Minute{}, false
}

func TestCollectorCounts(t *testing.T) {
	clk := newClock()
	c := newCollector(t, observability.WithInstance("inst-a"), observability.WithClock(clk.Now))
	rec := func(method, route string, status int, d time.Duration) {
		c.Record(observability.Request{Time: clk.Now(), Method: method, Route: route, Status: status, Duration: d})
	}
	rec("GET", "/v1/projects/{id}", 200, 10*time.Millisecond)
	rec("GET", "/v1/projects/{id}", 404, 2*time.Millisecond)
	rec("GET", "/v1/projects/{id}", 503, 30*time.Millisecond)
	rec("POST", "/v1/projects", 201, 50*time.Millisecond)
	rec("BREW", "/v1/projects", 400, time.Millisecond) // not a standard method
	rec("GET", "", 404, time.Millisecond)

	minutes := c.Minutes()
	if len(minutes) != 4 {
		t.Fatalf("Minutes() = %d series, want 4: %+v", len(minutes), minutes)
	}
	get, _ := find(minutes, "GET", "/v1/projects/{id}")
	if get.Requests != 3 || get.ClientErrors != 1 || get.ServerErrors != 1 || get.DurationSum != 42*time.Millisecond ||
		get.DurationMax != 30*time.Millisecond || get.Instance != "inst-a" || !get.Start.Equal(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("GET /v1/projects/{id} = %+v", get)
	}
	var inBuckets int64
	for _, n := range get.Buckets {
		inBuckets += n
	}
	if len(get.Buckets) != observability.BucketCount || inBuckets != 3 {
		t.Errorf("buckets = %v, want %d buckets holding 3 requests", get.Buckets, observability.BucketCount)
	}
	if other, ok := find(minutes, observability.OtherMethod, "/v1/projects"); !ok || other.ClientErrors != 1 {
		t.Errorf("BREW is not counted as %s: %+v", observability.OtherMethod, minutes)
	}
	if unmatched, ok := find(minutes, "GET", ""); !ok || unmatched.Requests != 1 {
		t.Errorf("unmatched request not counted under the empty route: %+v", minutes)
	}
}

func TestCollectorSeriesAreBounded(t *testing.T) {
	clk := newClock()
	c := newCollector(t, observability.WithClock(clk.Now), observability.WithMaxSeries(3))
	for i := range 100 {
		c.Record(observability.Request{Time: clk.Now(), Method: "GET", Route: fmt.Sprintf("/r%d", i), Status: 200})
	}
	minutes := c.Minutes()
	if len(minutes) != 4 {
		t.Fatalf("100 routes with max 3 series: %d series, want 3 and the overflow", len(minutes))
	}
	if over, ok := find(minutes, observability.OtherMethod, observability.OverflowRoute); !ok || over.Requests != 97 {
		t.Errorf("overflow series = %+v, want 97 requests", over)
	}
}

func TestCollectorFlushesMinutes(t *testing.T) {
	clk := newClock()
	sink := &memorySink{}
	c := newCollector(t, observability.WithClock(clk.Now), observability.WithSink(sink))
	ctx := context.Background()
	record := func(n int, status int) {
		for range n {
			c.Record(observability.Request{Time: clk.Now(), Method: "GET", Route: "/v1/ping", Status: status, Duration: time.Millisecond})
		}
	}

	record(5, 200)
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	record(3, 500) // the same minute, written again with its new totals
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := sink.total(); got.Requests != 8 || got.ServerErrors != 3 || len(sink.rows) != 1 {
		t.Errorf("after two flushes in one minute: %+v in %d rows, want 8 requests, 3 errors in one row", got, len(sink.rows))
	}

	clk.Add(time.Minute) // the next minute starts
	record(2, 200)
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := sink.total(); got.Requests != 10 || len(sink.rows) != 2 {
		t.Errorf("after the next minute: %+v in %d rows, want 10 requests in two rows", got, len(sink.rows))
	}
	// The finished minute is kept for one more write after the grace
	// period, then forgotten: memory stays bounded.
	clk.Add(2 * time.Second)
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(c.Minutes()); got != 1 {
		t.Errorf("after the finished minute's last write, %d series held, want only the current minute's", got)
	}

	// An idle instance forgets its last minute too.
	clk.Add(2 * time.Minute)
	for range 2 {
		if err := c.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		clk.Add(2 * time.Second)
	}
	if got := len(c.Minutes()); got != 0 {
		t.Errorf("idle collector holds %d series, want none", got)
	}
	if got := sink.total(); got.Requests != 10 {
		t.Errorf("sink = %+v, want the 10 requests unchanged", got)
	}
}

func TestCollectorKeepsBoundedMinutesWhileTheSinkFails(t *testing.T) {
	clk := newClock()
	sink := &memorySink{fail: errors.New("database unavailable")}
	c := newCollector(t, observability.WithClock(clk.Now), observability.WithSink(sink), observability.WithMaxPendingMinutes(3))
	ctx := context.Background()
	for range 10 {
		for range 4 {
			c.Record(observability.Request{Time: clk.Now(), Method: "GET", Route: "/v1/ping", Status: 200})
		}
		if err := c.Flush(ctx); err == nil {
			t.Fatal("Flush() with a failing sink error = nil")
		}
		clk.Add(time.Minute)
	}
	// Three finished minutes and the current one.
	if got := len(c.Minutes()); got != 4 {
		t.Errorf("held series = %d, want 4 (3 pending minutes and the last one)", got)
	}
	if c.Lost() != 24 {
		t.Errorf("Lost() = %d, want 24 (6 dropped minutes of 4 requests)", c.Lost())
	}

	sink.fail = nil
	clk.Add(2 * time.Second)
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	// The flush closes the current minute, which drops the oldest pending
	// one: 3 minutes are written.
	if got := sink.total().Requests; got != 12 || c.Lost() != 28 {
		t.Errorf("after the sink recovers: %d requests written, %d lost; want 12 and 28", got, c.Lost())
	}
}

func TestCollectorLateRequests(t *testing.T) {
	clk := newClock()
	sink := &memorySink{}
	c := newCollector(t, observability.WithClock(clk.Now), observability.WithSink(sink))
	first := clk.Now()
	c.Record(observability.Request{Time: first, Method: "GET", Route: "/a", Status: 200})
	clk.Add(time.Minute)
	c.Record(observability.Request{Time: clk.Now(), Method: "GET", Route: "/a", Status: 200})
	// Finished in the previous minute, still held: counted there.
	c.Record(observability.Request{Time: first, Method: "GET", Route: "/a", Status: 200})
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.Add(5 * time.Second)
	if err := c.Flush(context.Background()); err != nil { // forgets the first minute
		t.Fatal(err)
	}
	// Its minute is no longer held: counted in the latest minute instead of
	// overwriting what was written.
	c.Record(observability.Request{Time: first, Method: "GET", Route: "/a", Status: 200})
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, m := range sink.rows {
		if m.Requests != 2 {
			t.Errorf("minute %v = %d requests, want 2", m.Start, m.Requests)
		}
	}
	if len(sink.rows) != 2 {
		t.Errorf("rows = %d, want 2", len(sink.rows))
	}
}

func TestCollectorRunWritesOnStop(t *testing.T) {
	sink := &memorySink{}
	c := newCollector(t, observability.WithSink(sink), observability.WithFlushInterval(time.Minute))
	c.Record(observability.Request{Time: time.Now(), Method: "GET", Route: "/v1/ping", Status: 200})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- c.Run(ctx) }()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if sink.total().Requests != 1 || len(c.Minutes()) != 0 {
		t.Errorf("after Run stopped: %d requests written, %d series held; want 1 and 0", sink.total().Requests, len(c.Minutes()))
	}
}

func TestCollectorConcurrentRecords(t *testing.T) {
	clk := newClock()
	c := newCollector(t, observability.WithClock(clk.Now))
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 1000 {
				status := 200
				if i%10 == 0 {
					status = 500
				}
				c.Record(observability.Request{Time: clk.Now(), Method: "GET", Route: fmt.Sprintf("/r%d", g%3), Status: status, Duration: time.Duration(i) * time.Microsecond})
			}
		})
	}
	wg.Wait()
	var requests, errs int64
	for _, m := range c.Minutes() {
		requests += m.Requests
		errs += m.ServerErrors
	}
	if requests != 8000 || errs != 800 {
		t.Errorf("8 goroutines × 1000 requests: counted %d requests, %d errors; want 8000 and 800", requests, errs)
	}
}

func TestSubscribe(t *testing.T) {
	c := newCollector(t)
	var got []observability.Request
	unsubscribe := c.Subscribe(func(r observability.Request) { got = append(got, r) })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {})
	h := c.Middleware()(observability.RecordRoute(mux))
	req := httptest.NewRequest("GET", "/v1/items/42?secret=1", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	unsubscribe()
	unsubscribe() // twice is fine
	h.ServeHTTP(httptest.NewRecorder(), req)
	if len(got) != 1 || got[0].Path != "/v1/items/42" || got[0].Route != "/v1/items/{id}" || strings.Contains(fmt.Sprint(got[0]), "secret") {
		t.Errorf("subscriber got %+v, want one request with its path but not its query", got)
	}
}

func TestNewCollectorValidates(t *testing.T) {
	for name, opt := range map[string]observability.Option{
		"flush interval": observability.WithFlushInterval(time.Millisecond),
		"max series":     observability.WithMaxSeries(0),
		"instance":       observability.WithInstance(strings.Repeat("x", 65)),
		"logger":         observability.WithLogger(nil),
	} {
		if _, err := observability.NewCollector(opt); err == nil {
			t.Errorf("NewCollector with an invalid %s: error = nil", name)
		}
	}
	c := newCollector(t)
	if len(c.Instance()) != 16 {
		t.Errorf("default instance ID %q, want 16 random hex characters", c.Instance())
	}
}
