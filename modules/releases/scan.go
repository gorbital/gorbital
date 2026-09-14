package releases

import (
	"time"

	"github.com/jackc/pgx/v5"
)

// Instance is one start of an app instance.
type Instance struct {
	ID int64
	// InstanceID is random per process start.
	InstanceID string
	Version    string
	Commit     string
	// BuildTime is nil when the build didn't record one.
	BuildTime *time.Time
	// Modified reports a build from a tree with uncommitted changes.
	Modified   bool
	GoVersion  string
	Host       string
	StartedAt  time.Time
	LastSeenAt time.Time
	// StoppedAt is set when the instance shut down cleanly.
	StoppedAt *time.Time
	// Running reports that the instance hasn't stopped and sent a heartbeat
	// recently.
	Running bool
}

const instanceColumns = `id, instance_id, version, commit, build_time, modified, go_version, host, started_at, last_seen_at, stopped_at`

func scanInstance(row pgx.CollectableRow) (Instance, error) {
	var i Instance
	err := row.Scan(&i.ID, &i.InstanceID, &i.Version, &i.Commit, &i.BuildTime, &i.Modified, &i.GoVersion, &i.Host,
		&i.StartedAt, &i.LastSeenAt, &i.StoppedAt)
	i.StartedAt, i.LastSeenAt = i.StartedAt.UTC(), i.LastSeenAt.UTC()
	i.BuildTime, i.StoppedAt = utc(i.BuildTime), utc(i.StoppedAt)
	return i, err
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
