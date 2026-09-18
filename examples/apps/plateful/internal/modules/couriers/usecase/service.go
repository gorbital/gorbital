// Package usecase holds the couriers module's operations, one file each.
//
// Two kinds of caller reach them and the difference is the point of this
// module. A courier reaches their own profile as an ordinary signed-in
// account through guard.Permission(PermManage), and the use case compares
// the actor with the row's UserID, because a courier belongs to no
// organisation and guard.OrgMember has nothing to check. A restaurant's
// staff reach the dispatch list through guard.OrgMember(PermDispatch) on
// their own organisation's path, and read rows that belong to no
// organisation at all.
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

	"example.com/plateful/internal/modules/couriers/domain"
)

// docs:start courier-permissions

// Permissions the couriers routes require. Permission names are public API.
//
// The two are different kinds of permission, and a permission is one kind or
// the other, never both (module.go declares which).
//
// PermManage is a platform permission, held through the platform role
// "user", which every signed-in account has. It has to be: the routes it
// guards are a courier's own, a courier is in no organisation, and an
// organisation permission is only ever held by acting in an organisation. It
// is therefore a weak permission — it says "a signed-in person", not "this
// person" — so every use case behind it does its own ownership check
// against the profile's UserID.
//
// PermDispatch is an organisation permission, held by a restaurant's owners,
// admins and members through their role. It guards the dispatch list, which
// hangs under /v1/orgs/{orgId}/ so that guard.OrgMember can prove the caller
// is staff of a real restaurant before they see who is out delivering.
const (
	PermManage   = "couriers.courier.manage"
	PermDispatch = "couriers.courier.dispatch"
)

// docs:end courier-permissions

// Audit actions, public API: add new ones, never rename.
const (
	ActionRegistered = "couriers.courier.registered"
	ActionUpdated    = "couriers.courier.updated"
)

// Service runs the couriers use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing couriers in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "cur_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "cur_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// docs:start courier-caller-id

// callerID returns the signed-in account behind the request, whichever
// organisation it does or doesn't belong to.
//
// This is the couriers module's whole equivalent of restaurants' memberID,
// and the difference is instructive. memberID can assert a.OrgID == orgID,
// because guard.OrgMember put the organisation in the actor after proving
// the membership; there is nothing of the sort to assert here. The route's
// guard.Permission(PermManage) has proved only that somebody is signed in,
// so this returns the account and every caller of it compares that account
// with the courier row it read.
func callerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// docs:end courier-caller-id

// memberID returns the member acting in orgID, the organisation in the
// request's path: a restaurant's staff, or the organisation's own service
// account. guard.OrgMember checked the membership and the permission and
// left the organisation in the actor; a request acting in no organisation,
// or in another one, gets ErrUnauthenticated.
func memberID(ctx context.Context, orgID string) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" || orgID == "" || a.OrgID != orgID {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor and the
// request. There is no organisation on these events, because there is no
// organisation behind a courier.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "courier", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record couriers audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidCourier, domain.ErrCourierNotFound,
		domain.ErrCourierAlreadyRegistered, domain.ErrCourierOnDelivery,
		domain.ErrCourierVersionConflict,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("couriers: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
