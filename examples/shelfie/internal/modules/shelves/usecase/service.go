// Package usecase holds the shelves module's operations, one file each:
// each finds the signed-in user, who owns the shelves it reads or changes,
// applies the domain rules, stores the result through the Store port and
// records an audit event. Routes check permissions with guards before a use
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

	"example.com/shelfie/internal/modules/shelves/domain"
)

// Permissions the shelves routes require. Every signed-in user holds them
// through the user role (module.go); an API key only when its scopes include
// them. Permission names are public API.
const (
	PermRead  = "shelves.shelf.read"
	PermWrite = "shelves.shelf.write"
)

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated = "shelves.shelf.created"
	ActionUpdated = "shelves.shelf.updated"
	ActionDeleted = "shelves.shelf.deleted"
)

// Service runs the shelves use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing shelves in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "shl_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "shl_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// ownerID returns the signed-in user, who owns the shelves an operation
// reads or changes. The routes let only authenticated callers through; a
// service account's API key owns no shelves.
func ownerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor and request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "shelf", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record shelves audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidShelf, domain.ErrShelfNotFound,
		domain.ErrShelfNameTaken, domain.ErrShelfVersionConflict,
		page.ErrInvalidSort,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("shelves: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
