// Package usecase holds the partners module's operations, one file each:
// recording a purchase a partner reported, and listing a reader's own.
package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"

	"example.com/shelfie/internal/modules/partners/domain"
)

// PermRead is the permission the purchases route requires; every signed-in
// reader holds it through the user role (module.go). Permission names are
// public API.
const PermRead = "partners.purchase.read"

// ActionRecorded is the audit action of a recorded purchase, public API:
// add new ones, never rename.
const ActionRecorded = "partners.purchase.recorded"

// Store reads and writes purchases; repository.Store implements it with SQL.
type Store interface {
	// InsertPurchase stores p and returns it. When the partner already sent
	// this event ID, it returns the purchase stored then and false.
	InsertPurchase(ctx context.Context, p domain.Purchase) (domain.Purchase, bool, error)
	// SelectPurchases returns a reader's purchases, newest first.
	SelectPurchases(ctx context.Context, userID string, limit int) ([]domain.Purchase, error)
}

// Service runs the partner use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing purchases in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "prc_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "prc_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// readerID returns the signed-in reader, who owns the purchases the list
// operation reads.
func readerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "partner_purchase", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record partners audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	if errors.Is(err, domain.ErrUnauthenticated) {
		return err
	}
	return fmt.Errorf("partners: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
