package ratelimitpg

import (
	"context"
	"crypto/sha256"
	"time"

	"gorbital.dev/ratelimit"
)

// takeSQL decides a request with GCRA in one statement. With T the emission
// interval and tolerance T × burst, a request at now is allowed when
// max(tat, now) + T − now ≤ tolerance, and then tat becomes max(tat, now) + T.
// A new key is always allowed. The last two columns come from the snapshot
// before the statement, for the retry time of a refused request.
//
// $1 key, $2 T and $3 tolerance in microseconds, $4 now (NULL: the database
// clock).
const takeSQL = `
	WITH clock AS (
		SELECT COALESCE($4::timestamptz, statement_timestamp()) AS now, $2::bigint * interval '1 microsecond' AS t
	), decided AS (
		INSERT INTO ratelimit_buckets AS b (key, tat)
		SELECT $1, clock.now + clock.t FROM clock
		ON CONFLICT (key) DO UPDATE
		SET tat = GREATEST(b.tat, EXCLUDED.tat - (SELECT t FROM clock)) + (SELECT t FROM clock)
		WHERE GREATEST(b.tat, EXCLUDED.tat - (SELECT t FROM clock)) + (SELECT t FROM clock) - (EXCLUDED.tat - (SELECT t FROM clock))
			<= $3::bigint * interval '1 microsecond'
		RETURNING tat
	)
	SELECT (SELECT tat FROM decided), (SELECT tat FROM ratelimit_buckets WHERE key = $1), (SELECT now FROM clock)`

// take decides a request for key under lim in the database.
func (s *Store) take(ctx context.Context, name, key string, lim ratelimit.Limit) (ratelimit.Decision, error) {
	ctx, cancel := context.WithTimeout(ctx, DecisionTimeout)
	defer cancel()

	interval, tolerance := gcra(lim)
	var clock *time.Time
	if s.now != nil {
		now := s.now()
		clock = &now
	}
	var (
		newTAT, oldTAT *time.Time
		now            time.Time
	)
	err := s.pool.QueryRow(ctx, takeSQL, hashKey(name, key), interval.Microseconds(), tolerance.Microseconds(), clock).Scan(&newTAT, &oldTAT, &now)
	if err != nil {
		return ratelimit.Decision{}, err
	}
	if newTAT != nil {
		return ratelimit.Decision{Allowed: true}, nil
	}
	return ratelimit.Decision{RetryAfter: retryAfter(oldTAT, now, interval, tolerance)}, nil
}

// gcra returns lim's emission interval and tolerance, in whole microseconds
// (the resolution of timestamptz), at least one microsecond.
func gcra(lim ratelimit.Limit) (interval, tolerance time.Duration) {
	interval = max(time.Duration(float64(time.Second)/lim.PerSecond).Truncate(time.Microsecond), time.Microsecond)
	return interval, interval * time.Duration(lim.Burst)
}

// retryAfter is how long until a refused request would be allowed.
func retryAfter(tat *time.Time, now time.Time, interval, tolerance time.Duration) time.Duration {
	if tat == nil {
		return interval
	}
	arrival := *tat
	if now.After(arrival) {
		arrival = now
	}
	return max(arrival.Add(interval).Sub(now)-tolerance, time.Microsecond)
}

// hashKey keeps email addresses and IPs out of the table, and separates
// limiters that share a key.
func hashKey(name, key string) []byte {
	sum := sha256.Sum256([]byte(name + "\x00" + key))
	return sum[:]
}

// deleteExpiredSQL removes up to $2 buckets that are full again.
const deleteExpiredSQL = `
	DELETE FROM ratelimit_buckets WHERE key IN (
		SELECT key FROM ratelimit_buckets WHERE tat < COALESCE($1::timestamptz, statement_timestamp()) LIMIT $2
	)`

// DeleteExpired removes up to limit buckets whose keys are back to a full
// budget, so the table holds only recently limited keys. Apps run it from a
// periodic job.
func (s *Store) DeleteExpired(ctx context.Context, limit int) (int64, error) {
	var clock *time.Time
	if s.now != nil {
		now := s.now()
		clock = &now
	}
	tag, err := s.pool.Exec(ctx, deleteExpiredSQL, clock, limit)
	return tag.RowsAffected(), err
}

// resetSQL forgets one key's bucket, so its next request is allowed as a
// new key's.
const resetSQL = `DELETE FROM ratelimit_buckets WHERE key = $1`

// Reset forgets the budget of key under the limiter named name, for
// operators unblocking a client (ADR-0070). It reports whether a bucket
// existed. Local and fallback limiters in memory keep their own state,
// which empties within the window.
func (s *Store) Reset(ctx context.Context, name, key string) (bool, error) {
	if name == "" || key == "" {
		return false, ratelimit.ErrEmptyKey
	}
	ctx, cancel := context.WithTimeout(ctx, DecisionTimeout)
	defer cancel()
	tag, err := s.pool.Exec(ctx, resetSQL, hashKey(name, key))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
