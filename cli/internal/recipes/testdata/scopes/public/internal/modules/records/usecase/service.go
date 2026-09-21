// Package usecase holds the records module's operations, one file each:
// each applies the domain rules, stores the result through the Store port
// and records an audit event. Reads are world-readable and run for callers
// who aren't signed in; writes need records.record.write, which the routes check
// before a use case runs (delivery/routes.go).
package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorbital.dev/audit"
	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
)

// The permission the records writes require. Reading needs none: the
// routes that read are public. Permission names are public API.
const (
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
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing records in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
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
