package auditpg

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// StatsGroup is what [Store.Stats] counts events by.
type StatsGroup string

// Groupings for [Store.Stats].
const (
	StatsByAction       StatsGroup = "action"
	StatsByOutcome      StatsGroup = "outcome"
	StatsByActorKind    StatsGroup = "actor_kind"
	StatsByResourceType StatsGroup = "resource_type"
	// StatsByDay counts events per UTC day, keyed YYYY-MM-DD.
	StatsByDay StatsGroup = "day"
)

// Bounds of a stats window.
const (
	DefaultStatsWindow = 7 * 24 * time.Hour
	MaxStatsWindow     = 90 * 24 * time.Hour
	// MaxStatsGroups is the most groups returned, largest first; the rest are
	// counted in Stats.Other. Days are never cut: a window has at most 91.
	MaxStatsGroups = 50
)

var statsKeys = map[StatsGroup]string{
	StatsByAction:       "action",
	StatsByOutcome:      "outcome",
	StatsByActorKind:    "actor_kind",
	StatsByResourceType: "resource_type",
	StatsByDay:          `to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD')`,
}

// StatsFilter selects the events to count: Filter's fields except Limit and
// Cursor. From and To default to the DefaultStatsWindow ending now and may be
// at most MaxStatsWindow apart.
type StatsFilter struct {
	Filter
	GroupBy StatsGroup
}

// Stats are event counts in a window.
type Stats struct {
	From, To time.Time
	GroupBy  StatsGroup
	Total    int64
	// Groups are the largest groups, most events first, or every day in
	// order for StatsByDay.
	Groups []StatsCount
	// Other counts events in groups beyond MaxStatsGroups.
	Other int64
}

// StatsCount is one group's count. Key is empty for events without a
// resource type.
type StatsCount struct {
	Key   string
	Count int64
}

// Stats counts events matching f by f.GroupBy. It returns an error wrapping
// [ErrInvalidFilter] for an unknown grouping or a window over
// MaxStatsWindow, or [ErrQueryTimeout].
func (s *Store) Stats(ctx context.Context, f StatsFilter) (Stats, error) {
	key, ok := statsKeys[f.GroupBy]
	if !ok {
		return Stats{}, fmt.Errorf("%w: group_by must be action, outcome, actor_kind, resource_type or day", ErrInvalidFilter)
	}
	f.Limit, f.Cursor = 0, ""
	if f.To.IsZero() {
		f.To = time.Now().UTC()
	}
	if f.From.IsZero() {
		f.From = f.To.Add(-DefaultStatsWindow)
	}
	if err := f.validate(); err != nil {
		return Stats{}, err
	}
	if f.To.Sub(f.From) > MaxStatsWindow {
		return Stats{}, fmt.Errorf("%w: from and to may be at most 90 days apart", ErrInvalidFilter)
	}

	where, args := f.conditions()
	order, limit := "n DESC, key", MaxStatsGroups
	if f.GroupBy == StatsByDay {
		order, limit = "key", int(MaxStatsWindow/(24*time.Hour))+1
	}
	// The window function totals every group before LIMIT cuts them.
	sql := fmt.Sprintf(`SELECT key, n, sum(n) OVER ()::bigint FROM (
		SELECT %s AS key, count(*) AS n FROM audit_events WHERE %s GROUP BY 1
	) g ORDER BY %s LIMIT %d`, key, strings.Join(where, " AND "), order, limit)

	// The query timeout bounds the query, so a broad window can't hold a
	// connection.
	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.pool.Query(qctx, sql, args...)
	if err != nil {
		return Stats{}, queryError(ctx, qctx, "count events", err)
	}
	defer rows.Close()
	stats := Stats{From: f.From.UTC(), To: f.To.UTC(), GroupBy: f.GroupBy, Groups: []StatsCount{}}
	var shown int64
	for rows.Next() {
		var c StatsCount
		if err := rows.Scan(&c.Key, &c.Count, &stats.Total); err != nil {
			return Stats{}, fmt.Errorf("auditpg: count events: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
		}
		stats.Groups = append(stats.Groups, c)
		shown += c.Count
	}
	if err := rows.Err(); err != nil {
		return Stats{}, queryError(ctx, qctx, "count events", err)
	}
	stats.Other = stats.Total - shown
	return stats, nil
}
