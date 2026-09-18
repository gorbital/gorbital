// Package usecase holds the reviews module's operations, one file each.
//
// The callers are the point of this module, and no two of them are the same
// kind. A review is written by a customer, who belongs to no organisation
// and is reached with a platform permission the "user" role holds
// (PermWrite). It is read by anybody at all, signed in or not: the public
// list takes no actor and checks nothing. It is hidden by platform staff
// (PermModerate). The organisation the row belongs to — the restaurant whose
// reputation it is — can do none of the three.
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

	"example.com/plateful/internal/modules/reviews/domain"
)

// docs:start review-permission-names

// Permissions the reviews routes require. Permission names are public API.
//
// Both are platform permissions, held through a platform role, and neither
// has OrgRoles. That is not an oversight: an organisation permission is only
// ever held by a member of the organisation acting in it, and the two people
// who touch a review — the diner who writes it and the moderator who hides
// it — are outside the restaurant's organisation by definition. There is no
// permission here for the restaurant's own staff, because there is nothing
// they are allowed to do to a review.
//
// The public list holds no permission at all. It is guard.Public(), so there
// is no actor to hold one.
const (
	PermWrite    = "reviews.review.write"
	PermModerate = "reviews.review.moderate"
)

// docs:end review-permission-names

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated = "reviews.review.created"
	ActionUpdated = "reviews.review.updated"
	ActionHidden  = "reviews.review.hidden"
)

// Service runs the reviews use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing reviews in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "rev_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "rev_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// callerID returns the signed-in account. Every caller of this module who
// isn't reading the public list is one: a customer or platform staff, both
// members of no organisation that matters here, so there is no memberID
// counterpart — the restaurants module has one because its staff routes are
// guarded by guard.OrgMember, and none of these routes is.
func callerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// docs:start review-audit

// audit records an event after the change it describes; a failed audit write
// is logged, not returned.
//
// orgID is passed rather than left to the recorder. The recorder fills the
// organisation in from the actor, and none of these actors acts in one, so
// without this the restaurant's own audit trail would never show that
// somebody reviewed it.
func (s *Service) audit(ctx context.Context, action, id, orgID string, metadata map[string]any) {
	e := audit.Event{
		Action: action, ResourceType: "review", ResourceID: id,
		OrgID: orgID, Outcome: audit.OutcomeSuccess, Metadata: metadata,
	}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record reviews audit event", "action", action, "err", err)
	}
}

// docs:end review-audit

// storeError returns the module's own errors as they are and hides the rest,
// such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidReview, domain.ErrReviewNotFound, domain.ErrReviewExists,
		domain.ErrOrderNotFound, domain.ErrOrderNotDelivered,
		domain.ErrReviewWindowClosed, domain.ErrReviewVersionConflict,
		page.ErrInvalidSort, page.ErrInvalidCursor,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("reviews: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
