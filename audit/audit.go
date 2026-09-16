// Package audit defines audit events and the [Recorder] contract that any
// module uses to record who did what to which resource, and whether it
// worked. Storage lives in separate modules (for example auditpg).
//
// Audit events are not application logs: they have their own retention,
// integrity and access rules (ADR-0026).
//
// Stability: stable (ADR-0015, ADR-0054).
package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/actor"
	"gorbital.dev/requestid"
)

// Outcome is the result of an audited action.
type Outcome string

// Outcomes.
const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied"
)

// An Event records one audited action.
type Event struct {
	// OccurredAt is set by recorders when zero.
	OccurredAt time.Time

	ActorKind  actor.Kind
	ActorID    string
	ActorLabel string

	// Action is a dotted, past-tense name namespaced by module, such as
	// "auth.session.revoked". Action names are public API (ADR-0015).
	Action string

	ResourceType string
	ResourceID   string
	Outcome      Outcome
	OrgID        string

	RequestID string
	TraceID   string
	IP        string
	UserAgent string

	// Metadata holds structured detail. Recorders apply redaction rules.
	Metadata map[string]any
}

var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Validate reports whether e has a well-formed action and a known outcome.
func (e Event) Validate() error {
	var errs []error
	if !actionPattern.MatchString(e.Action) {
		errs = append(errs, fmt.Errorf("action %q must be dotted lowercase, like module.resource.verb", e.Action))
	}
	switch e.Outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomeDenied:
	default:
		errs = append(errs, fmt.Errorf("outcome %q is not success, failure or denied", e.Outcome))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("audit: invalid event: %w", err)
	}
	return nil
}

// FromContext returns e with empty actor, organisation, request, trace, IP
// and user agent fields filled from ctx. The IP address and user agent come
// from [actor.WithClient].
func FromContext(ctx context.Context, e Event) Event {
	if a, ok := actor.From(ctx); ok {
		if e.ActorKind == "" {
			e.ActorKind, e.ActorID, e.ActorLabel = a.Kind, a.ID, a.Label
		}
		if e.OrgID == "" {
			e.OrgID = a.OrgID
		}
	}
	if e.ActorKind == "" {
		e.ActorKind = actor.KindAnonymous
	}
	if e.RequestID == "" {
		e.RequestID = requestid.From(ctx)
	}
	if e.TraceID == "" {
		if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
			e.TraceID = sc.TraceID().String()
		}
	}
	if c, ok := actor.ClientFrom(ctx); ok {
		if e.IP == "" {
			e.IP = c.IP
		}
		if e.UserAgent == "" {
			e.UserAgent = c.UserAgent
		}
	}
	return e
}

// A Recorder stores audit events. Implementations must be safe for
// concurrent use and return an error when the event can't be stored.
type Recorder interface {
	Record(ctx context.Context, e Event) error
}

// RecorderFunc adapts a function to the [Recorder] interface.
type RecorderFunc func(ctx context.Context, e Event) error

// Record calls f(ctx, e).
func (f RecorderFunc) Record(ctx context.Context, e Event) error { return f(ctx, e) }

// LogRecorder writes audit events to a structured logger. It is meant for
// development and apps without an audit store; metadata is not logged.
type LogRecorder struct {
	logger *slog.Logger
	now    func() time.Time
}

// NewLogRecorder returns a recorder logging to logger.
func NewLogRecorder(logger *slog.Logger) *LogRecorder {
	return &LogRecorder{logger: logger, now: time.Now}
}

// Record validates e, fills it from ctx and logs it.
func (r *LogRecorder) Record(ctx context.Context, e Event) error {
	e = FromContext(ctx, e)
	if err := e.Validate(); err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = r.now().UTC()
	}
	r.logger.LogAttrs(ctx, slog.LevelInfo, "audit event",
		slog.String("action", e.Action),
		slog.String("outcome", string(e.Outcome)),
		slog.String("actor_kind", string(e.ActorKind)),
		slog.String("actor_id", e.ActorID),
		slog.String("resource_type", e.ResourceType),
		slog.String("resource_id", e.ResourceID),
		slog.String("org_id", e.OrgID),
		slog.String("request_id", e.RequestID),
		slog.Time("occurred_at", e.OccurredAt),
	)
	return nil
}
