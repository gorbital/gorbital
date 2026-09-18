// Package usecase holds the restaurants module's operations, one file each.
// Three kinds of caller reach them, and the difference is the point of this
// module: a restaurant's own staff through guard.OrgMember, any signed-in
// customer browsing what is open, and platform staff suspending a
// restaurant through a platform permission.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/settings"
	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// docs:start restaurant-permissions

// Permissions the restaurants routes require. Permission names are public
// API.
//
// The first two are organisation permissions: a member holds them through
// their role in the restaurant's organisation (module.go). The last three
// are platform permissions, held through a platform role: every signed-in
// account has "user" and can browse, while overseeing and suspending belong
// to the platform's own staff. A permission is one or the other, never both.
const (
	PermRead    = "restaurants.restaurant.read"
	PermWrite   = "restaurants.restaurant.write"
	PermBrowse  = "restaurants.restaurant.browse"
	PermOversee = "restaurants.restaurant.oversee"
	PermSuspend = "restaurants.restaurant.suspend"
)

// docs:end restaurant-permissions

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated     = "restaurants.restaurant.created"
	ActionUpdated     = "restaurants.restaurant.updated"
	ActionSuspended   = "restaurants.restaurant.suspended"
	ActionUnsuspended = "restaurants.restaurant.unsuspended"
)

// Service runs the restaurants use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	// maxRadius is the restaurants.max_delivery_radius_m setting, read on
	// every write: operators change the platform's limit in /ops/settings
	// without a deploy, and restaurants already past it keep what they have
	// until they next save.
	maxRadius *settings.Setting[int]
	now       func() time.Time
}

// NewService returns a Service storing restaurants in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger, maxRadius *settings.Setting[int]) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, maxRadius: maxRadius, now: time.Now}
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// limit returns the platform's delivery radius limit in metres.
func (s *Service) limit(ctx context.Context) int {
	if s.maxRadius == nil {
		return 0 // no limit declared: only the domain's own rules apply
	}
	return s.maxRadius.Get(ctx)
}

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

// callerID returns the signed-in account, whichever organisation it does or
// doesn't belong to: a customer browsing restaurants, or platform staff.
// The route's guard.Permission has already checked the permission.
func callerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor, the
// organisation and the request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "restaurant", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record restaurants audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidRestaurant, domain.ErrRestaurantNotFound,
		domain.ErrRestaurantNameTaken, domain.ErrRestaurantVersionConflict,
		domain.ErrRestaurantSuspended, domain.ErrRestaurantNotSuspended,
		domain.ErrSuspensionIsPlatformOnly,
		page.ErrInvalidSort,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("restaurants: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
