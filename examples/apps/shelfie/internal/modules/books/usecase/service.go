// Package usecase holds the books module's operations, one file each: each
// finds the signed-in reader, applies the domain rules, stores the result
// through the Store port and records an audit event. Routes check
// permissions with guards before a use case runs (delivery/routes.go).
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
	"gorbital.dev/config"

	"example.com/shelfie/internal/modules/books/domain"
)

// Permissions the books routes require. Every signed-in user holds them
// through the user role; an API key only when its scopes include them.
// Permission names are public API.
const (
	PermRead  = "books.book.read"
	PermWrite = "books.book.write"
)

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated = "books.book.created"
	ActionUpdated = "books.book.updated"
	ActionDeleted = "books.book.deleted"
	// ActionShelfEmptied records one reader removing their whole shelf.
	ActionShelfEmptied = "books.shelf.emptied"
)

// docs:start service

// Service runs the books use cases. It is safe for concurrent use.
type Service struct {
	store      Store
	recorder   audit.Recorder
	logger     *slog.Logger
	shelfLimit config.Value[int] // nil: no limit
	now        func() time.Time
	newID      func() string
}

// NewService returns a Service storing books in store and recording changes
// in recorder, with at most shelfLimit books per reader, read on every new
// book (a runtime setting operators change in /ops). Store and recorder may
// be nil while the OpenAPI document is exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger, shelfLimit config.Value[int]) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, shelfLimit: shelfLimit, now: time.Now, newID: newID}
}

// docs:end service

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "bok_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "bok_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// readerID returns the signed-in reader, who owns the books an operation
// reads or changes. The routes let only authenticated callers through; a
// service account's API key has no shelf.
func readerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event about one book.
func (s *Service) audit(ctx context.Context, action, id string) {
	s.auditResource(ctx, action, "book", id)
}

// auditResource records an event after the change it describes; a failed
// audit write is logged, not returned. The recorder adds the actor and
// request.
func (s *Service) auditResource(ctx context.Context, action, resourceType, id string) {
	e := audit.Event{Action: action, ResourceType: resourceType, ResourceID: id, Outcome: audit.OutcomeSuccess}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record books audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	for _, known := range []error{domain.ErrBookNotFound, domain.ErrISBNTaken} {
		if errors.Is(err, known) {
			return err
		}
	}
	return fmt.Errorf("books: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
