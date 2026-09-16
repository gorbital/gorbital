// Package usecase holds the projects module's application logic. Each
// operation finds the signed-in user, who owns the projects it reads or
// changes, checks the permission (ADR-0058), applies the domain rules, stores
// the result and records an audit event (ADR-0039). Change it freely.
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

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

// Config holds the Service's dependencies.
type Config struct {
	// Required.
	Store    Store
	Recorder audit.Recorder

	// Optional.
	Logger *slog.Logger
	// Now is the clock and NewID makes IDs, for tests.
	Now   func() time.Time
	NewID func() string
}

// Service runs the projects use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service.
func NewService(c Config) (*Service, error) {
	if c.Store == nil || c.Recorder == nil {
		return nil, errors.New("projects: invalid service: store and audit recorder are required")
	}
	s := &Service{store: c.Store, recorder: c.Recorder, logger: c.Logger, now: c.Now, newID: c.NewID}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.newID == nil {
		s.newID = newID
	}
	return s, nil
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "prj_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "prj_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// ownerID returns the signed-in user's ID when they hold permission. Only
// users own projects; system and anonymous actors get
// ErrUnauthenticated. Every user holds the permissions through the user
// role, but an API key only when its scopes include them, so a key limited
// to reading can't change anything (ADR-0058); it gets ErrForbidden.
func ownerID(ctx context.Context, permission string) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", projectsdomain.ErrUnauthenticated
	}
	if err := actor.Require(ctx, permission); err != nil {
		return "", projectsdomain.ErrForbidden
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor and request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "project", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record projects audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the rest,
// such as driver errors, which aren't API (ADR-0018).
func storeError(op string, err error) error {
	known := []error{
		projectsdomain.ErrInvalidProject, projectsdomain.ErrProjectNotFound,
		projectsdomain.ErrProjectNameTaken, projectsdomain.ErrProjectVersionConflict,
		page.ErrInvalidSort,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("projects: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}
