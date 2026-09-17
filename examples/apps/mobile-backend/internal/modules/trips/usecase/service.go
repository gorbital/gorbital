// Package usecase holds the trips module's operations, one file each: each
// applies the domain rules, reaches the store through the Store port and
// records an audit event when it changes something. Routes check
// permissions with guards before a use case runs (delivery/routes.go); the
// use cases scope every read and write to the caller.
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
)

// Permissions the routes require. They are the names the identity provider
// puts in the token's permissions claim, so the provider's configuration
// and this constant have to agree. Permission names are public API.
const (
	PermRead  = "trips.trip.read"
	PermWrite = "trips.trip.write"
)

// Audit actions of the trips module, public API: add new actions, never
// rename.
const (
	ActionAdded   = "trips.trip.added"
	ActionDeleted = "trips.trip.deleted"
)

// MaxList is the most trips ListTrips returns.
const MaxList = 100

// Service runs the trips use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing trips in store and recording changes
// in recorder. Store and recorder may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "trp_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "trp_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// errNoActor reports a request without an actor, which the routes' guards
// prevent: it isn't mapped, so it answers 500.
var errNoActor = errors.New("trips: no actor in the request")

// docs:start traveller

// travellerID returns whose trips the request reaches: the actor the
// authenticator put in the context, whose ID is the "sub" claim of the
// token the identity provider issued. Every use case starts here, and
// passes the ID to the store, so a query can't forget whose rows it reads.
func travellerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", errNoActor
	}
	return a.ID, nil
}

// docs:end traveller

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor and request.
func (s *Service) audit(ctx context.Context, action, id string) {
	e := audit.Event{Action: action, ResourceType: "trip", ResourceID: id, Outcome: audit.OutcomeSuccess}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record trips audit event", "action", action, "err", err)
	}
}

// storeError hides driver errors, which aren't API.
func storeError(op string, err error) error {
	return fmt.Errorf("trips: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
