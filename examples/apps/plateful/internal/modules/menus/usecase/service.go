// Package usecase holds the menus module's operations, one file each. Two
// kinds of caller reach them, and the difference is the point of this
// module: a restaurant's own staff, who see every dish including the ones
// the kitchen has switched off, and a customer in no organisation at all,
// who sees the published menu of an open restaurant.
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

	"example.com/plateful/internal/modules/menus/domain"
)

// docs:start menu-permissions

// Permissions the menus routes require. Permission names are public API.
//
// The first two are organisation permissions: a member holds them through
// their role in the restaurant's organisation (module.go). PermBrowse is a
// platform permission, held through the "user" role that every signed-in
// account has, because the customer who reads a menu belongs to no
// organisation at all. A permission is one or the other, never both.
const (
	PermRead   = "menus.item.read"
	PermWrite  = "menus.item.write"
	PermBrowse = "menus.menu.browse"
)

// docs:end menu-permissions

// Audit actions, public API: add new ones, never rename.
const (
	ActionCreated = "menus.item.created"
	ActionUpdated = "menus.item.updated"
	ActionDeleted = "menus.item.deleted"
)

// Service runs the menus use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing menu items in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "mnu_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "mnu_" + idEncoding.EncodeToString(b)
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

// callerID returns the signed-in account, whichever organisation it does or
// doesn't belong to: a customer reading a menu. The route's
// guard.Permission has already checked the permission.
func callerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit write
// is logged, not returned. The recorder adds the actor, the organisation and
// the request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "menu_item", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record menus audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the rest,
// such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidItem, domain.ErrItemNotFound, domain.ErrItemNameTaken,
		domain.ErrItemVersionConflict, domain.ErrRestaurantNotFound,
		page.ErrInvalidSort,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("menus: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}

// sortName is a sort as a cursor records it, so a cursor can't be replayed
// against a different order.
func sortName(f page.SortField) string {
	if f.Desc {
		return "-" + f.Field
	}
	return f.Field
}
