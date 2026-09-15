package releases

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"apistock.dev/buildinfo"
)

// writeTimeout bounds each write, including marking the instance stopped
// during shutdown.
const writeTimeout = 5 * time.Second

// Tracker records one app instance: its build when it starts, a heartbeat
// while it runs and the time it stops. Run it once, as an app.Runner.
type Tracker struct {
	pool *pgxpool.Pool
	cfg  config
	row  instanceRow
	id   int64 // the instance's row; 0 until recorded
}

// NewTracker returns a tracker for an instance running the build info
// describes, usually [buildinfo.Read]. An empty version is recorded as dev.
func NewTracker(pool *pgxpool.Pool, info buildinfo.Info, opts ...Option) (*Tracker, error) {
	cfg, err := newConfig(opts)
	if pool == nil {
		err = errors.Join(err, errors.New("pool is required"))
	}
	if err != nil {
		return nil, fmt.Errorf("releases: invalid tracker: %w", err)
	}
	host := cfg.host
	if !cfg.hostSet {
		host, _ = os.Hostname()
	}
	row := instanceRow{
		version:   cleanText(info.Version, maxVersionLen),
		commit:    cleanText(info.Commit, maxCommitLen),
		modified:  info.Modified,
		goVersion: cleanText(info.GoVersion, maxGoVersionLen),
		host:      cleanText(host, maxHostLen),
		// Chosen once, so the instance keeps its ID if its row is recorded
		// again.
		instanceID: newInstanceID(),
	}
	if row.version == "" {
		row.version = "dev"
	}
	if built, err := time.Parse(time.RFC3339, info.BuildTime); err == nil {
		built = built.UTC()
		row.buildTime = &built
	}
	return &Tracker{pool: pool, cfg: cfg, row: row}, nil
}

// InstanceID returns the random ID this instance is recorded under, as
// listed by the release log's instances.
func (t *Tracker) InstanceID() string { return t.row.instanceID }

// Run records the instance, sends heartbeats until ctx ends, then marks the
// instance stopped. It always returns nil: failed writes are logged and
// retried at the next heartbeat, so tracking never stops the app.
func (t *Tracker) Run(ctx context.Context) error {
	t.start(ctx)
	ticker := time.NewTicker(t.cfg.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.stop(context.WithoutCancel(ctx))
			return nil
		case <-ticker.C:
			t.beat(ctx)
		}
	}
}

// start records a new instance row, then deletes instances last seen before
// the retention.
func (t *Tracker) start(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	now := t.cfg.now().UTC()
	row := t.row
	row.startedAt = now
	id, err := insertInstance(ctx, t.pool, row)
	if err != nil {
		t.cfg.logger.ErrorContext(ctx, "record release instance", "version", row.version, "err", err)
		return
	}
	t.id = id
	if _, err := deleteInstancesSeenBefore(ctx, t.pool, now.Add(-t.cfg.retention)); err != nil {
		t.cfg.logger.ErrorContext(ctx, "delete old release instances", "err", err)
	}
}

// beat updates the instance's last heartbeat, recording the instance first
// if that hasn't worked yet, or again if its row is gone.
func (t *Tracker) beat(ctx context.Context) {
	if t.id == 0 {
		t.start(ctx)
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	found, err := touchInstance(writeCtx, t.pool, t.id, t.cfg.now().UTC())
	switch {
	case err != nil:
		t.cfg.logger.ErrorContext(ctx, "send release heartbeat", "err", err)
	case !found:
		t.id = 0
		t.start(ctx)
	}
}

// stop marks the instance stopped.
func (t *Tracker) stop(ctx context.Context) {
	if t.id == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := stopInstance(ctx, t.pool, t.id, t.cfg.now().UTC()); err != nil {
		t.cfg.logger.ErrorContext(ctx, "mark release instance stopped", "err", err)
	}
	t.id = 0
}

func newInstanceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}
