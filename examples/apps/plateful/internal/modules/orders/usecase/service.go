// Package usecase holds the orders module's operations, one file each.
//
// One table, three kinds of caller, and three different rules about who may
// touch a row — which is the whole reason this module exists in the example:
//
//   - restaurant staff reach their own restaurant's orders through
//     guard.OrgMember, and the organisation comes from the request's path;
//   - a customer reaches the orders they placed, at restaurants they are not
//     a member of, so guard.OrgMember can't be the guard and the ownership
//     check is here, comparing the actor against the order's customer_id;
//   - a courier reaches the one order assigned to them, and nothing else,
//     through their courier profile.
//
// The route's guard decides that the caller is signed in and holds a
// permission. Which rows that caller may see is a rule, and rules live here.
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
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"
	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start order-permissions

// Permissions the orders routes require. Permission names are public API.
//
// PermRead and PermManage are organisation permissions: a restaurant's staff
// hold them through their role. PermPlace, PermView and PermDeliver are
// platform permissions held by the "user" role, which every signed-in
// account has — because a customer and a courier belong to no organisation,
// and there is no role on this platform that means "customer". They say the
// route is for a signed-in human, not that this human may touch this row;
// that second question is answered in the use case.
const (
	PermRead    = "orders.order.read"
	PermManage  = "orders.order.manage"
	PermPlace   = "orders.order.place"
	PermView    = "orders.order.view"
	PermDeliver = "orders.order.deliver"
)

// docs:end order-permissions

// Audit actions, public API: add new ones, never rename.
const (
	ActionPlaced          = "orders.order.placed"
	ActionAccepted        = "orders.order.accepted"
	ActionRejected        = "orders.order.rejected"
	ActionAdvanced        = "orders.order.advanced"
	ActionCourierAssigned = "orders.order.courier_assigned"
	ActionCancelled       = "orders.order.cancelled"
	ActionDelivered       = "orders.order.delivered"
)

// Service runs the orders use cases. It is safe for concurrent use.
type Service struct {
	store Store
	// tx runs the writes that must happen together: an order, its lines, the
	// stock it takes and the job that tells the restaurant (ports.go).
	tx       TxManager
	recorder audit.Recorder
	logger   *slog.Logger
	// maxOpen, lateAfter, scheduled and autoAssign are the module's runtime
	// settings and feature flags, declared in module.go and read when an
	// operation runs, so /ops/settings and /ops/flags change behaviour
	// without a deploy.
	maxOpen    *settings.Setting[int]
	lateAfter  *settings.Setting[time.Duration]
	scheduled  *flags.Flag
	autoAssign *flags.Flag
	now        func() time.Time
	newID      func() string
}

// Config is what NewService needs besides its stores: the module's settings
// and flags, declared before the stores exist.
type Config struct {
	MaxOpen    *settings.Setting[int]
	LateAfter  *settings.Setting[time.Duration]
	Scheduled  *flags.Flag
	AutoAssign *flags.Flag
}

// NewService returns a Service reading orders from store and writing what
// must change together through tx. store, tx and recorder may be nil while
// the OpenAPI document is exported, when no use case runs; tx is also nil in
// the service the orders_late_sweep job keeps, which only reads, because the
// job client a transaction would enqueue through doesn't exist yet when a
// module's Jobs runs.
func NewService(store Store, tx TxManager, recorder audit.Recorder, logger *slog.Logger, cfg Config) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		store: store, tx: tx, recorder: recorder, logger: logger,
		maxOpen: cfg.MaxOpen, lateAfter: cfg.LateAfter, scheduled: cfg.Scheduled, autoAssign: cfg.AutoAssign,
		now: time.Now, newID: newID,
	}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "ord_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "ord_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// memberID returns the member acting in orgID, the restaurant's organisation
// in the request's path. guard.OrgMember checked the membership and the
// permission and left the organisation in the actor.
func memberID(ctx context.Context, orgID string) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" || orgID == "" || a.OrgID != orgID {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// docs:start caller-id

// callerID returns the signed-in account, whatever organisation it does or
// doesn't belong to: a customer, or a courier. guard.Permission has checked
// that the caller is signed in and holds a platform permission; it says
// nothing about rows, so every use case that starts here goes on to compare
// this ID against the order.
func callerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// docs:end caller-id

// docs:start order-audit

// audit records an event after the change it describes; a failed audit
// write is logged, not returned.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "order", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record orders audit event", "action", action, "err", err)
	}
}

// docs:end order-audit

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidOrder, domain.ErrOrderNotFound, domain.ErrInvalidTransition,
		domain.ErrRestaurantNotFound, domain.ErrRestaurantNotAccepting, domain.ErrRestaurantBusy,
		domain.ErrItemUnavailable, domain.ErrItemOutOfStock, domain.ErrPaymentNotAuthorised,
		domain.ErrCourierUnavailable, domain.ErrNotACourier, domain.ErrSchedulingUnavailable,
		page.ErrInvalidSort, page.ErrInvalidCursor,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("orders: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
