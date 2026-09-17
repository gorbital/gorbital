// Package usecase holds the invoices module's operations, one file each:
// each works in the organisation of the request's path, finds the member
// acting in it, applies the domain rules, stores the result through the
// Store port, limited to that organisation, and records an audit event.
// Routes check membership and permissions with guard.OrgMember before a use
// case runs (delivery/routes.go).
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
	"gorbital.dev/page"

	"example.com/invoicing/internal/modules/invoices/domain"
)

// Permissions the invoices routes require in an organisation. Every member
// holds them through their role (module.go); an API key only when its scopes
// include them. Permission names are public API.
const (
	PermRead  = "invoices.invoice.read"
	PermWrite = "invoices.invoice.write"
)

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated = "invoices.invoice.created"
	ActionUpdated = "invoices.invoice.updated"
	ActionDeleted = "invoices.invoice.deleted"
)

// Service runs the invoices use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing invoices in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "inv_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "inv_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// memberID returns the member acting in orgID, the organisation in the
// request's path: a user, or the organisation's own service account.
// guard.OrgMember checked the membership and the permission, and left the
// organisation in the actor; a request acting in no organisation or in
// another one, such as on a route without the guard, gets
// ErrUnauthenticated.
func memberID(ctx context.Context, orgID string) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" || orgID == "" || a.OrgID != orgID {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor, the
// organisation and the request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "invoice", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record invoices audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidInvoice, domain.ErrInvoiceNotFound,
		domain.ErrInvoiceNumberTaken, domain.ErrInvoiceVersionConflict,
		page.ErrInvalidSort,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("invoices: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
