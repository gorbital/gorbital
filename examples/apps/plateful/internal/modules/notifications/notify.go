package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"gorbital.dev/modules/jobs"

	"example.com/plateful/internal/modules/notifications/usecase"
)

// Job names, public API. They are re-exported from the use cases so that
// other modules and the tests name them without reaching into a layer they
// aren't allowed to import: a module may take another module's root package
// and nothing below it.
const (
	FanoutJob   = usecase.FanoutJob
	DeliveryJob = usecase.DeliveryJob
)

// docs:start notify-api

// Fanout returns the job arguments that tell one organisation something.
//
// It returns arguments rather than doing the work because the caller's
// transaction is the only place the decision can be made safely: an order
// module inserts its order and enqueues this in the same
// jobs.Client.InsertTx, so the notification exists if and only if the order
// does. A helper that inserted on its own would notify a restaurant about an
// order that rolled back.
//
//	if _, err := tx.Jobs().InsertTx(ctx, tx, notifications.Fanout(orgID,
//		"New order", []string{"Order " + order.ID, "Total £24.50"}), nil); err != nil {
//		return err
//	}
func Fanout(orgID, title string, lines []string) river.JobArgs {
	return usecase.FanoutArgs{OrgID: orgID, Title: title, Lines: lines}
}

// FromWorker enqueues the same thing from inside a running worker, taking
// the job client from ctx.
//
// This exists because gorbital.Deps.Jobs is nil while jobs are being
// defined, so a worker cannot be handed a client at startup; River puts its
// own in the context it works a job with. A job that runs on a schedule —
// "which orders are late?" — uses this.
func FromWorker(ctx context.Context, orgID, title string, lines []string) error {
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("notifications: notify: no job client in this context: %w", err)
	}
	if _, err := client.Insert(ctx, Fanout(orgID, title, lines), nil); err != nil {
		return fmt.Errorf("notifications: notify: %w", err)
	}
	return nil
}

// With returns a notifier that enqueues through c, for code that holds a
// client — a command, a start-up task — and for tests. The returned function
// does not join a transaction: prefer [Fanout] with InsertTx where there is
// one.
func With(c *jobs.Client) func(ctx context.Context, orgID, title string, lines []string) error {
	return func(ctx context.Context, orgID, title string, lines []string) error {
		if c == nil {
			return errors.New("notifications: notify: no job client")
		}
		if _, err := c.Insert(ctx, Fanout(orgID, title, lines), nil); err != nil {
			return fmt.Errorf("notifications: notify: %w", err)
		}
		return nil
	}
}

// docs:end notify-api
