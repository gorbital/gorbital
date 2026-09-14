// Package releases records which build every instance of an app runs and
// answers which releases are running (ADR-0040).
//
// A [Tracker] is an app.Runner: it records the instance's build when it
// starts, sends a heartbeat while it runs, and marks the instance stopped
// when its context ends. A [Store] queries the records for operator APIs:
//
//	tracker, err := releases.NewTracker(pool, buildinfo.Read())
//	store, err := releases.NewStore(pool)
//	current, err := store.Current(ctx)
//
// An instance is running while it hasn't stopped and its last heartbeat is
// recent, so instances that crash or lose their network stop counting after
// three missed heartbeats. Tracking never stops an app from starting or
// serving: failed writes are logged and retried at the next heartbeat.
//
// The release_instances table comes from [Migrations]; apply them first.
//
// Stability: pre-1.0 (ADR-0015).
package releases

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Defaults.
const (
	DefaultHeartbeat = 30 * time.Second
	DefaultRetention = 90 * 24 * time.Hour
)

const (
	minHeartbeat = 5 * time.Second
	maxHeartbeat = 5 * time.Minute
	minRetention = 24 * time.Hour
	maxRetention = 3 * 365 * 24 * time.Hour

	// runningHeartbeats is how long, in heartbeats, an instance counts as
	// running after it was last seen.
	runningHeartbeats = 3

	// Text column limits, in characters. Longer values are truncated.
	maxVersionLen   = 100
	maxCommitLen    = 64
	maxGoVersionLen = 32
	maxHostLen      = 255
)

// ErrInvalidCursor reports a cursor that wasn't returned by a [Store] list.
var ErrInvalidCursor = errors.New("releases: invalid cursor")

type config struct {
	heartbeat time.Duration
	retention time.Duration
	host      string
	hostSet   bool
	logger    *slog.Logger
	now       func() time.Time
}

// An Option configures [NewTracker] and [NewStore].
type Option interface{ apply(*config) }

type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// WithHeartbeat sets how often a tracker updates its instance, from 5
// seconds to 5 minutes. Give the store the same value: it counts an instance
// as running for three heartbeats after it was last seen. Default:
// [DefaultHeartbeat].
func WithHeartbeat(d time.Duration) Option {
	return optionFunc(func(c *config) { c.heartbeat = d })
}

// WithRetention sets how long instances are kept after they were last seen,
// from 1 day to 3 years. A tracker deletes older instances when it starts.
// Default: [DefaultRetention].
func WithRetention(d time.Duration) Option {
	return optionFunc(func(c *config) { c.retention = d })
}

// WithHost sets the host a tracker records, such as a pod name. Default: the
// operating system's host name.
func WithHost(host string) Option {
	return optionFunc(func(c *config) { c.host, c.hostSet = host, true })
}

// WithLogger sets the logger for failed writes. Default: discard.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(c *config) { c.logger = logger })
}

// WithClock sets the clock, for tests.
func WithClock(now func() time.Time) Option {
	return optionFunc(func(c *config) { c.now = now })
}

func newConfig(opts []Option) (config, error) {
	c := config{heartbeat: DefaultHeartbeat, retention: DefaultRetention, now: time.Now}
	for _, opt := range opts {
		opt.apply(&c)
	}
	if c.logger == nil {
		c.logger = slog.New(slog.DiscardHandler)
	}
	var errs []error
	if c.heartbeat < minHeartbeat || c.heartbeat > maxHeartbeat {
		errs = append(errs, fmt.Errorf("heartbeat %v must be between %v and %v", c.heartbeat, minHeartbeat, maxHeartbeat))
	}
	if c.retention < minRetention || c.retention > maxRetention {
		errs = append(errs, fmt.Errorf("retention %v must be between %v and %v", c.retention, minRetention, maxRetention))
	}
	if c.now == nil {
		errs = append(errs, errors.New("clock must not be nil"))
	}
	return c, errors.Join(errs...)
}

// cleanText makes s storable in a text column: valid UTF-8, no NUL bytes, at
// most limit characters.
func cleanText(s string, limit int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", ""))
	n := 0
	for i := range s {
		if n == limit {
			return s[:i]
		}
		n++
	}
	return s
}

// dbError hides driver errors, which aren't API (ADR-0018).
func dbError(op string, err error) error {
	return fmt.Errorf("releases: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}
