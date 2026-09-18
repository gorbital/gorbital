package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"example.com/plateful/internal/modules/notifications/domain"
)

// FanoutJob is the name of the job that turns "tell this organisation
// something" into one delivery per endpoint. Job names are public API:
// renaming one orphans its configuration overrides and its history.
const FanoutJob = "notification_fanout"

// FanoutArgs are the job's arguments, stored as JSON with the job.
//
// The message travels with the job rather than being looked up later,
// because unlike a payment there is no row to read it back from: "order
// ord_7 was placed at 19:04" is a thing that happened, not a thing that is
// stored. Nothing personal goes in it — the callers send an order reference
// and a time, not a customer's address — because job arguments sit in the
// queue table until the job is pruned.
type FanoutArgs struct {
	OrgID string   `json:"org_id"`
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// Kind returns [FanoutJob].
func (FanoutArgs) Kind() string { return FanoutJob }

// FanoutWorker reads an organisation's endpoints and enqueues one delivery
// each. module.go defines the job with it.
type FanoutWorker struct {
	river.WorkerDefaults[FanoutArgs]
	store  Store
	client Enqueuer
	logger *slog.Logger
}

// NewFanoutWorker returns a worker reading endpoints from store. client may
// be nil, and is in production: the worker then takes River's own client
// from the context it is worked with. A test passes one so it can drive the
// worker without River running.
func NewFanoutWorker(store Store, client Enqueuer, logger *slog.Logger) *FanoutWorker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &FanoutWorker{store: store, client: client, logger: logger}
}

// docs:start fanout-work

// Work turns one message for one organisation into one delivery job per
// endpoint.
//
// The split is the point. A restaurant with three channels gets three jobs,
// each with its own attempt count and its own backoff, so a channel whose
// server is down is retried on its own and the two that worked are not
// posted to again. One job posting to all three would have to choose between
// re-sending to everybody and remembering half a delivery itself, and both
// of those are worse than letting the job system count.
//
// An organisation with no endpoints is a success, not an error: most
// restaurants never register one, and a discarded job per order would bury
// the real failures.
func (w *FanoutWorker) Work(ctx context.Context, job *river.Job[FanoutArgs]) error {
	if (domain.Message{Title: job.Args.Title, Lines: job.Args.Lines}).IsEmpty() {
		// Nothing to say. Cancelling rather than failing keeps a caller's bug
		// out of the operators' list of jobs to look at.
		return river.JobCancel(fmt.Errorf("notifications: fanout: the message is empty"))
	}
	endpoints, err := w.store.SelectEndpoints(ctx, job.Args.OrgID)
	if err != nil {
		return storeError("fanout", err)
	}
	if len(endpoints) == 0 {
		return nil
	}
	client, err := w.enqueuer(ctx)
	if err != nil {
		return err
	}
	// Enqueueing is at-least-once: if the fifth insert fails, this attempt
	// returns an error and the retry enqueues all five again, so an endpoint
	// may be told twice. That is the right way round for a notification — a
	// duplicate alert is a nuisance, a missed order is a lost dinner — and
	// the alternative, remembering how far it got, is exactly the hand-rolled
	// bookkeeping the job system exists to avoid.
	for _, e := range endpoints {
		args := DeliveryArgs{
			EndpointID: e.ID,
			OrgID:      e.OrgID,
			Title:      job.Args.Title,
			Lines:      job.Args.Lines,
		}
		if _, err := client.Insert(ctx, args, nil); err != nil {
			return fmt.Errorf("notifications: fanout: enqueue delivery for %s: %w", e.ID, err)
		}
	}
	w.logger.DebugContext(ctx, "notification fanout", "org_id", job.Args.OrgID, "endpoints", len(endpoints))
	return nil
}

// docs:end fanout-work

// enqueuer returns the client this worker inserts delivery jobs with.
//
// gorbital.Deps.Jobs is always nil inside Module.Jobs — the job client is
// built from the definitions, so it cannot be handed to a worker while they
// are being declared — which means a worker that needs to enqueue has to
// find its client at work time. River puts its own client in the context it
// works a job with, and ClientFromContextSafely returns an error instead of
// panicking when it isn't there, which is what makes the worker drivable
// from a test.
func (w *FanoutWorker) enqueuer(ctx context.Context) (Enqueuer, error) {
	if w.client != nil {
		return w.client, nil
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return nil, fmt.Errorf("notifications: fanout: no job client in this context: %w", err)
	}
	return client, nil
}
