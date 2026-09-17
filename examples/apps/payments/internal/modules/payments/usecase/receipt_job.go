package usecase

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// docs:start receipt-args

// ReceiptJob is the name of the job that sends a payment's receipt. Job
// names are public API: renaming one orphans its configuration overrides
// and its history.
const ReceiptJob = "payment_receipt"

// ReceiptArgs are the job's arguments, stored as JSON with the job. They
// name the payment rather than repeating it, so the worker reads the row
// that was committed and no personal data sits in the queue.
type ReceiptArgs struct {
	PaymentID string `json:"payment_id"`
}

// Kind returns [ReceiptJob].
func (ReceiptArgs) Kind() string { return ReceiptJob }

// docs:end receipt-args

// ReceiptWorker sends the receipt for a recorded payment. module.go defines
// the job with it.
type ReceiptWorker struct {
	river.WorkerDefaults[ReceiptArgs]
	store  Store
	logger *slog.Logger
}

// NewReceiptWorker returns a worker reading payments from store.
func NewReceiptWorker(store Store, logger *slog.Logger) *ReceiptWorker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &ReceiptWorker{store: store, logger: logger}
}

// docs:start receipt-work

// Work sends one receipt. The job exists only because its transaction
// committed, so the payment is always there to read; an error retries the
// job, up to the attempts the definition allows.
func (w *ReceiptWorker) Work(ctx context.Context, job *river.Job[ReceiptArgs]) error {
	payment, err := w.store.SelectPayment(ctx, job.Args.PaymentID)
	if err != nil {
		return storeError("receipt", err)
	}
	// Where a real app emails the customer, posts to the ledger or calls
	// the accounting system. Whatever it is, it happens here and not in the
	// request: the provider gets its 200 as soon as the row is committed.
	w.logger.InfoContext(ctx, "receipt sent",
		"payment_id", payment.ID, "status", string(payment.Status),
		"amount_minor", payment.AmountMinor, "currency", payment.Currency)
	return nil
}

// docs:end receipt-work
