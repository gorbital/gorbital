package jobs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/rivertype"
)

// selectJobForRetrySQL locks the job, so its state can't change between the
// check and the retry.
const selectJobForRetrySQL = `SELECT kind, state::text FROM river_job WHERE id = $1 FOR UPDATE`

// selectJobForRetry returns the job's kind and state, or [ErrJobNotFound].
func selectJobForRetry(ctx context.Context, tx pgx.Tx, id int64) (string, rivertype.JobState, error) {
	var kind, state string
	err := tx.QueryRow(ctx, selectJobForRetrySQL, id).Scan(&kind, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrJobNotFound
	}
	return kind, rivertype.JobState(state), err
}
