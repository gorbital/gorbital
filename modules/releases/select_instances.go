package releases

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"apistock.dev/modules/postgres"
)

// InstanceFilter selects instance starts. Empty fields match everything.
type InstanceFilter struct {
	Version string
	Commit  string
	// RunningOnly keeps the instances running now.
	RunningOnly bool
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is InstancePage.NextCursor from the previous page.
	Cursor string
}

// InstancePage is a page of instance starts, newest first.
type InstancePage struct {
	Instances []Instance
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}

// CurrentRelease is a release with the instances running it now.
type CurrentRelease struct {
	Version   string
	Commit    string
	Instances []Instance
}

// maxCurrentInstances bounds the running instances Current reads.
const maxCurrentInstances = 1000

// Instances returns instance starts matching f, newest first. It returns
// [ErrInvalidCursor] for a cursor it didn't return.
func (s *Store) Instances(ctx context.Context, f InstanceFilter) (InstancePage, error) {
	limit := pageLimit(f.Limit)
	var before int64
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil || id < 1 {
			return InstancePage{}, ErrInvalidCursor
		}
		before = id
	}
	list, err := selectInstances(ctx, s.pool, f, s.runningSince(), before, limit+1)
	if err != nil {
		return InstancePage{}, dbError("list instances", err)
	}
	s.markRunning(list)
	result := InstancePage{Instances: list}
	if len(list) > limit {
		result.Instances = list[:limit]
		result.NextCursor = strconv.FormatInt(result.Instances[limit-1].ID, 10)
	}
	return result, nil
}

// Current returns the releases running now, newest start first, each with
// its running instances. During a rolling deploy it returns more than one;
// when no instance sent a recent heartbeat, none.
func (s *Store) Current(ctx context.Context) ([]CurrentRelease, error) {
	list, err := selectInstances(ctx, s.pool, InstanceFilter{RunningOnly: true}, s.runningSince(), 0, maxCurrentInstances)
	if err != nil {
		return nil, dbError("list running instances", err)
	}
	s.markRunning(list)
	current := []CurrentRelease{}
	index := map[[2]string]int{}
	for _, instance := range list {
		key := [2]string{instance.Version, instance.Commit}
		if i, ok := index[key]; ok {
			current[i].Instances = append(current[i].Instances, instance)
			continue
		}
		index[key] = len(current)
		current = append(current, CurrentRelease{Version: instance.Version, Commit: instance.Commit, Instances: []Instance{instance}})
	}
	return current, nil
}

const selectInstancesSQL = `SELECT ` + instanceColumns + ` FROM release_instances`

// selectInstances appends one fixed condition per set filter.
func selectInstances(ctx context.Context, db postgres.DBTX, f InstanceFilter, runningSince time.Time, before int64, limit int) ([]Instance, error) {
	var (
		where []string
		args  []any
	)
	add := func(condition string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if f.Version != "" {
		add("version = $%d", f.Version)
	}
	if f.Commit != "" {
		add("commit = $%d", f.Commit)
	}
	if f.RunningOnly {
		add("stopped_at IS NULL AND last_seen_at >= $%d", runningSince)
	}
	if before > 0 {
		add("id < $%d", before)
	}

	sql := selectInstancesSQL
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanInstance)
}
