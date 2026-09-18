package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"gorbital.dev/audit"

	"example.com/plateful/internal/modules/notifications/domain"
)

// DeliveryJob is the name of the job that posts one message to one endpoint.
// Job names are public API.
const DeliveryJob = "notification_delivery"

// DeliveryArgs are the job's arguments. They name the endpoint rather than
// carrying its URL: the secret stays in the database, where it is one row
// the worker reads and nothing else does, instead of sitting in the queue
// table in plain text for as long as the job history is kept.
type DeliveryArgs struct {
	EndpointID string   `json:"endpoint_id"`
	OrgID      string   `json:"org_id"`
	Title      string   `json:"title"`
	Lines      []string `json:"lines"`
}

// Kind returns [DeliveryJob].
func (DeliveryArgs) Kind() string { return DeliveryJob }

// DeliveryWorker posts one message to one endpoint. module.go defines the
// job with it.
type DeliveryWorker struct {
	river.WorkerDefaults[DeliveryArgs]
	store    Store
	sender   Sender
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
}

// NewDeliveryWorker returns a worker reading endpoints from store and
// posting through sender. recorder and logger may be nil.
func NewDeliveryWorker(store Store, sender Sender, recorder audit.Recorder, logger *slog.Logger) *DeliveryWorker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &DeliveryWorker{store: store, sender: sender, recorder: recorder, logger: logger, now: time.Now}
}

// docs:start delivery-work

// Work posts one message to one endpoint and decides what the job system
// should do next. There are exactly three answers, and none of them is a
// loop in this function:
//
//   - nil: the endpoint answered 2xx, or there is nothing left to do.
//   - an ordinary error: the attempt may work next time, so River retries it
//     with its own backoff, up to the MaxAttempts the definition declares (5).
//     This is the retry loop, and it belongs to the job system: a `for` with
//     a `time.Sleep` in it would hold a worker slot for minutes, lose
//     everything on a deploy, and be invisible to the operator's job list.
//   - river.JobCancel: the attempt will never work, so stop now rather than
//     spend four more attempts and a discarded job proving it.
//
// The endpoint is read back on every attempt rather than trusted from the
// arguments, so a webhook URL a restaurant rotated — or revoked, by deleting
// the row — takes effect on the next attempt instead of the next order.
func (w *DeliveryWorker) Work(ctx context.Context, job *river.Job[DeliveryArgs]) error {
	e, err := w.store.SelectEndpointWithURL(ctx, job.Args.OrgID, job.Args.EndpointID)
	switch {
	case errors.Is(err, domain.ErrEndpointNotFound):
		// The restaurant removed the endpoint between the fanout and this
		// attempt. Cancelling is right and discarding would be wrong: a
		// discarded job sits in the dashboard as something an operator has to
		// look at, and this is a restaurant using the product correctly.
		return river.JobCancel(err)
	case err != nil:
		return storeError("delivery", err)
	}

	status, sendErr := w.sender.Send(ctx, e.URL, domain.Message{Title: job.Args.Title, Lines: job.Args.Lines})
	w.recordAttempt(ctx, e, status, sendErr)

	switch {
	case sendErr == nil:
		w.event(ctx, ActionSent, e, status, audit.OutcomeSuccess, "")
		return nil

	case errors.Is(sendErr, domain.ErrDeliveryRejected):
		// A 4xx that isn't 408 or 429, a redirect, or an address the policy
		// refuses to dial: the endpoint is wrong, not busy. Four more
		// attempts would be four more requests to someone else's server on
		// behalf of a restaurant whose webhook has been revoked.
		w.event(ctx, ActionFailed, e, status, audit.OutcomeFailure, sendErr.Error())
		return river.JobCancel(sendErr)
	}

	// Transient: a connection that failed, or 408, 429 or a 5xx. Returning an
	// ordinary error is the whole retry policy.
	//
	// The audit event is written only when this was the last attempt, so one
	// unreachable endpoint writes one notifications.delivery.failed and not
	// five. Five would not be five failures; they would be one failure
	// described five times, and an audit trail that inflates like that is one
	// nobody reads.
	if job.Attempt >= job.MaxAttempts {
		w.event(ctx, ActionFailed, e, status, audit.OutcomeFailure, sendErr.Error())
	}
	return sendErr
}

// docs:end delivery-work

// recordAttempt stores the outcome on the endpoint so the restaurant can see
// it in the API. A failed write is logged and swallowed: it must not turn a
// delivery that worked into a retry.
func (w *DeliveryWorker) recordAttempt(ctx context.Context, e domain.Endpoint, status int, sendErr error) {
	failure := ""
	if sendErr != nil {
		// The sender's errors name the host and the status and never the URL,
		// which is why this one can be stored in a column the API returns.
		failure = sendErr.Error()
	}
	next := e.Delivered(status, failure, w.now().UTC().Truncate(time.Microsecond))
	if err := w.store.UpdateDelivery(context.WithoutCancel(ctx), next); err != nil {
		w.logger.ErrorContext(ctx, "record notification delivery",
			"endpoint_id", e.ID, "org_id", e.OrgID, "host", e.URL.Host(), "err", err)
	}
}

// event records what a delivery attempt did. The metadata may carry the
// endpoint's ID, the host it points at and the status it answered; it may
// never carry the URL.
func (w *DeliveryWorker) event(ctx context.Context, action string, e domain.Endpoint, status int, outcome audit.Outcome, failure string) {
	metadata := map[string]any{"endpoint_id": e.ID, "host": e.URL.Host(), "status": status}
	if failure != "" {
		metadata["error"] = failure
	}
	record(ctx, w.recorder, w.logger, systemEvent(DeliveryJob, action, e.OrgID, e.ID, outcome, metadata))
}
