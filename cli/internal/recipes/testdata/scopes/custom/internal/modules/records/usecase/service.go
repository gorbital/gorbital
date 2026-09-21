// Package usecase holds the records module's operations, one file each:
// each applies the domain rules, stores the result through the Store port
// and records an audit event. Who may see and change one record is the
// module's own rule: every operation asks the Policy port, which policy.go
// implements, and the routes' permissions are the floor under it, not the
// rule itself.
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
	"gorbital.dev/gorbital"
	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
)

// Permissions the records routes require before Policy is asked. Holding
// one lets a caller ask; the policy decides what they get. Permission names
// are public API.
const (
	PermRead  = "records.record.read"
	PermWrite = "records.record.write"
)

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated = "records.record.created"
	ActionUpdated = "records.record.updated"
	ActionDeleted = "records.record.deleted"
)

// Service runs the records use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	policy   Policy
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing records in store, asking policy who
// may see and change them, and recording changes in recorder. All three may
// be nil while the OpenAPI document is exported, when no use case runs.
func NewService(store Store, policy Policy, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, policy: policy, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "rcr_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "rcr_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// actorID returns who is acting, which the policy usually compares against.
// The routes let only authenticated callers through.
func actorID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor and request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "record", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record records audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		// An unwritten policy is a refusal of its own, answered with 501.
		gorbital.ErrNotImplemented,
		domain.ErrInvalidRecord, domain.ErrRecordNotFound,
		domain.ErrRecordTitleTaken, domain.ErrRecordVersionConflict,
		page.ErrInvalidSort,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("records: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
