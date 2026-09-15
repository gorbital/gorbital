package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/actor"
	"gorbital.dev/requestid"
)

// metadataKey namespaces gorbital's data inside River job metadata.
const metadataKey = "gorbital"

// jobContext is the correlation data stored with each job (ADR-0030).
// Permissions are deliberately absent.
type jobContext struct {
	RequestID   string `json:"request_id,omitempty"`
	TraceParent string `json:"traceparent,omitempty"`
	TraceState  string `json:"tracestate,omitempty"`
	ActorKind   string `json:"actor_kind,omitempty"`
	ActorID     string `json:"actor_id,omitempty"`
	ActorLabel  string `json:"actor_label,omitempty"`
	OrgID       string `json:"org_id,omitempty"`
}

// correlation carries request ID, trace and actor from enqueue to work.
type correlation struct {
	river.MiddlewareDefaults
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

var (
	_ rivertype.JobInsertMiddleware = (*correlation)(nil)
	_ rivertype.WorkerMiddleware    = (*correlation)(nil)
)

func newCorrelation(tp trace.TracerProvider, propagator propagation.TextMapPropagator) *correlation {
	return &correlation{tracer: tp.Tracer(instrumentationName), propagator: propagator}
}

// InsertMany adds the enqueuing context to every job's metadata.
func (m *correlation) InsertMany(ctx context.Context, params []*rivertype.JobInsertParams,
	doInner func(context.Context) ([]*rivertype.JobInsertResult, error),
) ([]*rivertype.JobInsertResult, error) {
	if jc := m.capture(ctx); jc != (jobContext{}) {
		for _, p := range params {
			md, err := withJobContext(p.Metadata, jc)
			if err != nil {
				return nil, fmt.Errorf("jobs: add metadata to %s job: %w", p.Kind, err)
			}
			p.Metadata = md
		}
	}
	return doInner(ctx)
}

// Work restores the enqueuing context and wraps the attempt in a span.
func (m *correlation) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	jc := readJobContext(job.Metadata)
	if requestid.Valid(jc.RequestID) {
		ctx = requestid.With(ctx, jc.RequestID)
	}
	ctx = m.propagator.Extract(ctx, propagation.MapCarrier{"traceparent": jc.TraceParent, "tracestate": jc.TraceState})
	ctx, span := m.tracer.Start(ctx, "job "+job.Kind,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "river"),
			attribute.String("messaging.destination.name", job.Queue),
			attribute.Int64("job.id", job.ID),
			attribute.String("job.kind", job.Kind),
			attribute.Int("job.attempt", job.Attempt),
		),
	)
	defer span.End()

	system := actor.System("jobs")
	system.OrgID = jc.OrgID
	ctx = actor.With(ctx, system)
	if jc.ActorKind != "" {
		ctx = context.WithValue(ctx, onBehalfOfKey{}, actor.Actor{
			Kind: actor.Kind(jc.ActorKind), ID: jc.ActorID, Label: jc.ActorLabel, OrgID: jc.OrgID,
		})
	}

	err := doInner(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "job failed")
	}
	return err
}

func (m *correlation) capture(ctx context.Context) jobContext {
	carrier := propagation.MapCarrier{}
	m.propagator.Inject(ctx, carrier)
	jc := jobContext{
		RequestID:   requestid.From(ctx),
		TraceParent: carrier.Get("traceparent"),
		TraceState:  carrier.Get("tracestate"),
	}
	// A job enqueued by another job keeps the original actor.
	a, ok := OnBehalfOf(ctx)
	if !ok {
		a, ok = actor.From(ctx)
	}
	if ok {
		jc.ActorKind, jc.ActorID, jc.ActorLabel, jc.OrgID = string(a.Kind), a.ID, a.Label, a.OrgID
	}
	return jc
}

type onBehalfOfKey struct{}

// OnBehalfOf returns the actor who enqueued the job running with ctx. Inside
// a job the context actor is actor.System("jobs"); record this actor in audit
// metadata. It has no permissions: authorise work when enqueuing.
func OnBehalfOf(ctx context.Context) (actor.Actor, bool) {
	a, ok := ctx.Value(onBehalfOfKey{}).(actor.Actor)
	return a, ok
}

func withJobContext(metadata []byte, jc jobContext) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &fields); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(jc)
	if err != nil {
		return nil, err
	}
	fields[metadataKey] = encoded
	return json.Marshal(fields)
}

// readJobContext ignores malformed metadata: correlation is best effort and
// must never fail a job.
func readJobContext(metadata []byte) jobContext {
	var fields map[string]json.RawMessage
	var jc jobContext
	if json.Unmarshal(metadata, &fields) == nil {
		_ = json.Unmarshal(fields[metadataKey], &jc)
	}
	return jc
}
