// Package usecase holds the notifications module's operations, one file
// each: registering, listing and removing the endpoints a restaurant is told
// through, and the two jobs that do the telling.
//
// gorbital has no outbound webhooks and no notification channel but email,
// so everything below the HTTP layer here is the app's own: the delivery
// attempt, the classification of what is worth retrying, and the audit trail
// of what was sent. What the framework does give — jobs with bounded
// retries, audit, org-scoped guards — is used rather than re-implemented,
// which is the whole point of the shape.
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

	"example.com/plateful/internal/modules/notifications/domain"
)

// Permissions the notifications routes require. Permission names are public
// API.
//
// Both are organisation permissions, held through a role in the restaurant's
// organisation. Neither is granted to "member": an endpoint URL is a
// credential for the restaurant's own chat, and kitchen staff have no
// business seeing, or changing, where a restaurant's alerts go. Only owners
// and admins do.
const (
	PermRead  = "notifications.endpoint.read"
	PermWrite = "notifications.endpoint.write"
)

// docs:start notification-audit

// Audit actions, public API: add new ones, never rename. The first two are
// recorded by a request, the last two by the delivery worker.
const (
	ActionRegistered = "notifications.endpoint.registered"
	ActionRemoved    = "notifications.endpoint.removed"
	ActionSent       = "notifications.delivery.sent"
	ActionFailed     = "notifications.delivery.failed"
)

// resourceType is what the audit events are about.
const resourceType = "notification_endpoint"

// audit records an event after the change it describes; a failed audit write
// is logged, not returned. The recorder adds the actor, the organisation and
// the request from the context.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	record(ctx, s.recorder, s.logger, audit.Event{
		Action: action, ResourceType: resourceType, ResourceID: id,
		Outcome: audit.OutcomeSuccess, Metadata: metadata,
	})
}

// systemEvent returns the audit event for something a job did.
//
// A worker has no request and no actor: nothing called actor.WithActor on
// its context, so audit.FromContext would file the event under the anonymous
// actor with no organisation, which is wrong in the one place an operator
// most wants the trail to be right. So the job names itself — the actor is
// the system, identified by the job's own name — and the organisation is set
// explicitly from the job's arguments rather than inferred.
func systemEvent(jobName, action, orgID, id string, outcome audit.Outcome, metadata map[string]any) audit.Event {
	return audit.Event{
		ActorKind:    actor.KindSystem,
		ActorID:      jobName,
		ActorLabel:   jobName,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   id,
		Outcome:      outcome,
		OrgID:        orgID,
		Metadata:     metadata,
	}
}

// record stores e, logging a failure rather than returning it: an audit
// write that fails must not undo the work it describes. The context is
// stripped of cancellation so an event still lands when the request, or the
// job's timeout, has already ended.
func record(ctx context.Context, recorder audit.Recorder, logger *slog.Logger, e audit.Event) {
	if recorder == nil {
		return
	}
	if err := recorder.Record(context.WithoutCancel(ctx), e); err != nil && logger != nil {
		logger.ErrorContext(ctx, "record notifications audit event", "action", e.Action, "err", err)
	}
}

// docs:end notification-audit

// Service runs the notifications use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	recorder audit.Recorder
	logger   *slog.Logger
	// policy decides which targets this deployment accepts, and is the only
	// thing module.go's option changes. It is read at registration; the
	// sender applies it again when it dials.
	policy domain.Policy
	now    func() time.Time
	newID  func() string
}

// NewService returns a Service storing endpoints in store and recording
// changes in recorder. Both may be nil while the OpenAPI document is
// exported, when no use case runs.
func NewService(store Store, recorder audit.Recorder, logger *slog.Logger, policy domain.Policy) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, recorder: recorder, logger: logger, policy: policy, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "nte_" and 128 random bits.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "nte_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// memberID returns the member acting in orgID, the organisation in the
// request's path. guard.OrgMember checked the membership and the permission
// and left the organisation in the actor; a request acting in no
// organisation or in another one gets ErrUnauthenticated.
func memberID(ctx context.Context, orgID string) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" || orgID == "" || a.OrgID != orgID {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// storeError returns the module's own errors as they are and hides the rest,
// such as driver errors, which aren't API.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidEndpoint, domain.ErrInvalidURL,
		domain.ErrEndpointNotFound, domain.ErrEndpointLabelTaken,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("notifications: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
