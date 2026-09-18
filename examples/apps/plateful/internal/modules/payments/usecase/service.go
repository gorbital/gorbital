// Package usecase holds the payments module's operations, one file each.
// Three very different callers reach them, and the difference is the point
// of this module: a customer, who is a member of no organisation and is
// recognised by owning the order; the payment provider, which has no
// account at all and is trusted only because its delivery is signed; and
// platform staff refunding, with a platform permission and a recent
// reauthentication.
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

	"example.com/plateful/internal/modules/payments/domain"
)

// Permissions the payments routes require. Permission names are public API.
//
// Both are platform permissions, not organisation ones. PermPay is held by
// the "user" role, which every signed-in account has, because a customer
// belongs to no organisation and guard.OrgMember could never let them in:
// the check that this order is theirs is in PayOrder, not in a guard.
// PermRefund belongs to the platform's own staff, who act on a tenant
// rather than within one.
const (
	PermPay    = "payments.payment.pay"
	PermRefund = "payments.payment.refund"
)

// Audit actions, public API: add new ones, never rename. The last four are
// recorded with no signed-in actor behind them — the provider's webhook and
// the platform's refund both act on a customer's money without the customer
// being there.
const (
	ActionCreated    = "payments.payment.created"
	ActionAuthorised = "payments.payment.authorised"
	ActionCaptured   = "payments.payment.captured"
	ActionFailed     = "payments.payment.failed"
	ActionRefunded   = "payments.payment.refunded"
)

// actionFor returns the audit action that records a move to status.
func actionFor(status domain.Status) string {
	switch status {
	case domain.StatusAuthorised:
		return ActionAuthorised
	case domain.StatusCaptured:
		return ActionCaptured
	case domain.StatusFailed:
		return ActionFailed
	case domain.StatusRefunded:
		return ActionRefunded
	default:
		return ActionCreated
	}
}

// Service runs the payments use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	provider Provider
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing payments in store, asking provider
// for the money and recording changes in recorder. All three may be nil
// while the OpenAPI document is exported, when no use case runs.
func NewService(store Store, provider Provider, recorder audit.Recorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, provider: provider, recorder: recorder, logger: logger, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "pay_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "pay_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// callerID returns the signed-in account, which belongs to no organisation:
// a customer paying for their own order. The route's guard.Permission has
// already checked the permission; who owns the order is the use case's
// business.
func callerID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder fills in the actor and the
// organisation from the request, which is exactly what a webhook doesn't
// have, so every event here names both itself.
func (s *Service) audit(ctx context.Context, e audit.Event) {
	e.ResourceType, e.Outcome = "payment", audit.OutcomeSuccess
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record payments audit event", "action", e.Action, "err", err)
	}
}

// docs:start payment-service-actor

// serviceEvent is the audit event of a change nobody was signed in for: the
// provider's webhook, which carries no session, and no organisation either.
// Both have to be set by hand — the recorder can only copy an actor the
// request has, and a webhook's request has none — so the trail says "the
// payments service, acting for this restaurant" rather than nothing at all.
func serviceEvent(action string, p domain.Payment) audit.Event {
	return audit.Event{
		Action:     action,
		ResourceID: p.ID,
		ActorKind:  actor.KindService,
		ActorID:    "payments_provider_webhook",
		ActorLabel: "Payment provider webhook",
		OrgID:      p.OrgID,
	}
}

// docs:end payment-service-actor

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidPayment, domain.ErrPaymentNotFound, domain.ErrOrderNotFound,
		domain.ErrPaymentNotPayable, domain.ErrPaymentAlreadyFinal, domain.ErrPaymentNotRefundable,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("payments: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
