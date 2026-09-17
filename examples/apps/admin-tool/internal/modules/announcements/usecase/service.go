// Package usecase holds the announcements module's operations, one file
// each: each applies the domain rules, stores the result through the Store
// port and records an audit event when it changes something. Routes check
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
)

// PermWrite is the permission publishing requires. Staff hold it through
// the platform_admin role; reading announcements is public. Permission
// names are public API.
const PermWrite = "announcements.announcement.write"

// The audit actions of the announcements module, public API: add new
// actions, never rename. Operators find them in GET /ops/audit under the
// action_prefix announcements..
const (
	ActionPublished = "announcements.announcement.published"
	ActionWithdrawn = "announcements.announcement.withdrawn"
)

// MaxList is the most announcements ListAnnouncements returns.
const MaxList = 50

// Service runs the announcements use cases. It is safe for concurrent use.
type Service struct {
	store     Store
	recorder  audit.Recorder
	logger    *slog.Logger
	maxActive config.Value[int] // nil: no limit
	now       func() time.Time
	newID     func() string
}

// NewService returns a Service storing announcements in store and recording
// changes in recorder, with at most maxActive active announcements, read on
// every publish (a runtime setting operators change in /ops). Store and
// recorder may be nil while the OpenAPI document is exported, when no use
// case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger, maxActive config.Value[int]) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, maxActive: maxActive, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "ann_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "ann_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// errNoActor reports a publish without an actor, which the route's guard
// prevents: it isn't mapped, so it answers 500.
var errNoActor = errors.New("announcements: no actor in the request")

// publisherID returns who publishes: the signed-in staff member.
func publisherID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", errNoActor
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor and request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "announcement", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record announcements audit event", "action", action, "err", err)
	}
}

// storeError hides driver errors, which aren't API.
func storeError(op string, err error) error {
	return fmt.Errorf("announcements: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
