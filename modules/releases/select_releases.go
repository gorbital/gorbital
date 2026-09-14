package releases

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"apistock.dev/page"
)

// Release is one build: every instance start with the same version and
// commit.
type Release struct {
	Version        string
	Commit         string
	FirstStartedAt time.Time
	LastSeenAt     time.Time
	// Running counts the instances running now.
	Running int
	// Starts counts every recorded start, including stopped instances.
	Starts int
	// Modified reports that an instance ran a build with uncommitted changes.
	Modified bool
}

// ReleaseFilter pages through releases.
type ReleaseFilter struct {
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is ReleasePage.NextCursor from the previous page.
	Cursor string
}

// ReleasePage is a page of releases, newest first.
type ReleasePage struct {
	Releases []Release
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}

type releaseCursor struct {
	StartedAt time.Time `json:"t"`
	Version   string    `json:"v"`
	Commit    string    `json:"c"`
}

const selectReleasesSQL = `
	SELECT version, commit, min(started_at), max(last_seen_at),
	       count(*) FILTER (WHERE stopped_at IS NULL AND last_seen_at >= $1), count(*), bool_or(modified)
	FROM release_instances
	GROUP BY version, commit
	HAVING NOT $2::boolean OR (min(started_at), version, commit) < ($3::timestamptz, $4::text, $5::text)
	ORDER BY min(started_at) DESC, version DESC, commit DESC
	LIMIT $6`

// Releases returns releases, newest first by their first start. It returns
// [ErrInvalidCursor] for a cursor it didn't return.
func (s *Store) Releases(ctx context.Context, f ReleaseFilter) (ReleasePage, error) {
	limit := pageLimit(f.Limit)
	var after releaseCursor
	if f.Cursor != "" {
		if err := page.DecodeCursor(f.Cursor, &after); err != nil || after.StartedAt.IsZero() {
			return ReleasePage{}, ErrInvalidCursor
		}
	}
	rows, err := s.pool.Query(ctx, selectReleasesSQL, s.runningSince(), f.Cursor != "", after.StartedAt, after.Version, after.Commit, limit+1)
	if err != nil {
		return ReleasePage{}, dbError("list releases", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Release, error) {
		var r Release
		err := row.Scan(&r.Version, &r.Commit, &r.FirstStartedAt, &r.LastSeenAt, &r.Running, &r.Starts, &r.Modified)
		r.FirstStartedAt, r.LastSeenAt = r.FirstStartedAt.UTC(), r.LastSeenAt.UTC()
		return r, err
	})
	if err != nil {
		return ReleasePage{}, dbError("list releases", err)
	}
	result := ReleasePage{Releases: list}
	if len(list) > limit {
		result.Releases = list[:limit]
		last := result.Releases[limit-1]
		cursor, err := page.EncodeCursor(releaseCursor{StartedAt: last.FirstStartedAt, Version: last.Version, Commit: last.Commit})
		if err != nil {
			return ReleasePage{}, err
		}
		result.NextCursor = cursor
	}
	return result, nil
}
