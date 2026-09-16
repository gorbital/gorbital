package observability

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// writeMinutesSQL upserts minutes passed as parallel arrays, one statement
// per batch. Totals are absolute, so writing a minute again replaces its
// row; a write with fewer requests than stored (a snapshot older than the
// stored one) changes nothing. Buckets arrive as array literals.
const writeMinutesSQL = `
	INSERT INTO observability_minutes AS m (minute, instance_id, method, route, requests, client_errors, server_errors,
		duration_sum_us, duration_max_us, buckets, updated_at)
	SELECT u.minute, u.instance_id, u.method, u.route, u.requests, u.client_errors, u.server_errors,
		u.duration_sum_us, u.duration_max_us, u.buckets::bigint[], statement_timestamp()
	FROM unnest($1::timestamptz[], $2::text[], $3::text[], $4::text[], $5::bigint[], $6::bigint[], $7::bigint[],
		$8::bigint[], $9::bigint[], $10::text[])
		AS u(minute, instance_id, method, route, requests, client_errors, server_errors, duration_sum_us, duration_max_us, buckets)
	ON CONFLICT (minute, instance_id, method, route) DO UPDATE
	SET requests = EXCLUDED.requests, client_errors = EXCLUDED.client_errors, server_errors = EXCLUDED.server_errors,
		duration_sum_us = EXCLUDED.duration_sum_us, duration_max_us = EXCLUDED.duration_max_us,
		buckets = EXCLUDED.buckets, updated_at = EXCLUDED.updated_at
	WHERE EXCLUDED.requests >= m.requests`

// WriteMinutes implements [Sink]: it stores minutes, replacing earlier
// writes of the same instance, minute, method and route.
func (s *Store) WriteMinutes(ctx context.Context, minutes []Minute) error {
	for len(minutes) > 0 {
		n := min(len(minutes), writeBatch)
		if err := s.writeBatch(ctx, minutes[:n]); err != nil {
			return err
		}
		minutes = minutes[n:]
	}
	return nil
}

func (s *Store) writeBatch(ctx context.Context, minutes []Minute) error {
	n := len(minutes)
	starts := make([]time.Time, n)
	instances, methods, routes, buckets := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	requests, clientErrors, serverErrors := make([]int64, n), make([]int64, n), make([]int64, n)
	sumMicros, maxMicros := make([]int64, n), make([]int64, n)
	var literal strings.Builder
	for i, m := range minutes {
		starts[i] = m.Start.Truncate(time.Minute).UTC()
		instances[i], methods[i], routes[i] = m.Instance, m.Method, m.Route
		requests[i], clientErrors[i], serverErrors[i] = m.Requests, m.ClientErrors, m.ServerErrors
		sumMicros[i], maxMicros[i] = m.DurationSum.Microseconds(), m.DurationMax.Microseconds()

		literal.Reset()
		literal.WriteByte('{')
		for b := range BucketCount {
			if b > 0 {
				literal.WriteByte(',')
			}
			var n int64
			if b < len(m.Buckets) {
				n = m.Buckets[b]
			}
			literal.WriteString(strconv.FormatInt(n, 10))
		}
		literal.WriteByte('}')
		buckets[i] = literal.String()
	}
	_, err := s.pool.Exec(ctx, writeMinutesSQL, starts, instances, methods, routes,
		requests, clientErrors, serverErrors, sumMicros, maxMicros, buckets)
	if err != nil {
		return dbError("write minutes", err)
	}
	return nil
}
