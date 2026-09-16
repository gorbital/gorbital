package observability

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// selectSummarySQL adds up the minutes in [$1, $2) four ways in one scan:
// everything, per minute, per instance and per method and route. Buckets
// are summed element by element, one sum per bucket (bucketSums), which
// keeps the scan at one row per stored minute. Without minutes, no row is
// returned, not even the total.
//
// The first column is GROUPING(minute, instance_id, method, route): 15 for
// the total, 7 per minute, 11 per instance, 12 per route.
var selectSummarySQL = `
	SELECT GROUPING(minute, instance_id, method, route), minute, instance_id, method, route,
		sum(requests)::bigint, sum(client_errors)::bigint, sum(server_errors)::bigint, sum(duration_sum_us)::bigint,
		max(duration_max_us), max(updated_at), max(minute), ` + bucketSums() + `
	FROM observability_minutes
	WHERE minute >= $1 AND minute < $2
	GROUP BY GROUPING SETS ((), (minute), (instance_id), (method, route))
	HAVING count(*) > 0`

// bucketSums returns ARRAY[sum(buckets[1]), …]::bigint[] over every bucket.
func bucketSums() string {
	var b strings.Builder
	b.WriteString("ARRAY[")
	for i := range BucketCount {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "sum(buckets[%d])", i+1)
	}
	b.WriteString("]::bigint[]")
	return b.String()
}

// Grouping values of selectSummarySQL.
const (
	groupTotal    = 15
	groupMinute   = 7
	groupInstance = 11
	groupRoute    = 12
)

// Summary is every instance's requests in a time range.
type Summary struct {
	// From and To are the range's minutes: From inclusive, To exclusive.
	From, To time.Time
	Total    Stats
	// Minutes has one entry per minute with requests, oldest first.
	Minutes []MinuteStats
	// Instances has one entry per instance with requests, by instance ID.
	Instances []InstanceStats
	// Routes has one entry per method and route with requests, by route
	// and method.
	Routes []RouteStats
}

// MinuteStats are one minute's requests across instances.
type MinuteStats struct {
	Start time.Time
	Stats
}

// InstanceStats are one instance's requests.
type InstanceStats struct {
	Instance string
	// LastMinute is the latest minute with requests, and LastWrite when the
	// instance last wrote its minutes: an instance that stopped writing
	// has stopped or can't reach the database.
	LastMinute time.Time
	LastWrite  time.Time
	Stats
}

// RouteStats are one method and route's requests across instances.
type RouteStats struct {
	Method string
	// Route is the route pattern's path; empty for requests no route
	// matched, and [OverflowRoute] for requests beyond the series limit.
	Route string
	Stats
}

// Summary adds up the minutes starting in [from, to), both truncated to the
// minute. It returns [ErrInvalidRange] for an empty or reversed range or one
// longer than [MaxSummaryRange], and [ErrQueryTimeout] when the query takes
// longer than the query timeout.
func (s *Store) Summary(ctx context.Context, from, to time.Time) (Summary, error) {
	from, to = from.Truncate(time.Minute).UTC(), to.Truncate(time.Minute).UTC()
	if !to.After(from) || to.Sub(from) > MaxSummaryRange {
		return Summary{}, ErrInvalidRange
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, selectSummarySQL, from, to)
	if err != nil {
		return Summary{}, s.queryError(ctx, err)
	}
	sum := Summary{From: from, To: to}
	var (
		grouping                                                   int
		minute, lastMinute, updatedAt                              *time.Time
		instance, method, route                                    *string
		requests, clientErrors, serverErrors, sumMicros, maxMicros int64
		buckets                                                    []int64
	)
	_, err = pgx.ForEachRow(rows, []any{&grouping, &minute, &instance, &method, &route,
		&requests, &clientErrors, &serverErrors, &sumMicros, &maxMicros, &updatedAt, &lastMinute, &buckets}, func() error {
		st := Stats{
			Requests: requests, ClientErrors: clientErrors, ServerErrors: serverErrors,
			DurationSum: time.Duration(sumMicros) * time.Microsecond, DurationMax: time.Duration(maxMicros) * time.Microsecond,
			Buckets: buckets,
		}
		switch grouping {
		case groupTotal:
			sum.Total = st
		case groupMinute:
			sum.Minutes = append(sum.Minutes, MinuteStats{Start: minute.UTC(), Stats: st})
		case groupInstance:
			sum.Instances = append(sum.Instances, InstanceStats{Instance: *instance, LastMinute: lastMinute.UTC(), LastWrite: updatedAt.UTC(), Stats: st})
		case groupRoute:
			sum.Routes = append(sum.Routes, RouteStats{Method: *method, Route: *route, Stats: st})
		}
		buckets = nil // a new slice for the next row
		return nil
	})
	if err != nil {
		return Summary{}, s.queryError(ctx, err)
	}
	slices.SortFunc(sum.Minutes, func(a, b MinuteStats) int { return a.Start.Compare(b.Start) })
	slices.SortFunc(sum.Instances, func(a, b InstanceStats) int { return strings.Compare(a.Instance, b.Instance) })
	slices.SortFunc(sum.Routes, func(a, b RouteStats) int {
		return strings.Compare(a.Route+" "+a.Method, b.Route+" "+b.Method)
	})
	return sum, nil
}

func (s *Store) queryError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrQueryTimeout
	}
	return dbError("summarize minutes", err)
}
