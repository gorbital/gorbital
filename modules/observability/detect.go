package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"gorbital.dev/actor"
	"gorbital.dev/modules/postgres"
)

// Detection configures [Store.DetectIncident].
type Detection struct {
	// Window is how far back requests count, 1 minute to 1 hour.
	Window time.Duration
	// Threshold is the error rate above which an incident opens, from 0 to
	// 1: 0.05 for 5% of requests being server errors.
	Threshold float64
	// MinRequests is how many requests the window needs before its error
	// rate counts, at least 1: a few failures of a quiet app don't open
	// incidents, and no traffic never looks recovered.
	MinRequests int64
	// Severity of incidents detection opens. Default: sev2.
	Severity Severity
	// Actor is who detection acts as in incidents' timelines, such as
	// actor.System("incidents_detect").
	Actor actor.Actor
}

// DetectionAction is what a detection changed.
type DetectionAction string

// Detection actions.
const (
	// DetectionNone: nothing changed.
	DetectionNone DetectionAction = ""
	// DetectionOpened: an automatic incident was opened.
	DetectionOpened DetectionAction = "opened"
	// DetectionRecovered: the open automatic incident's error rate
	// recovered; an update says so. The incident stays open for operators
	// to resolve.
	DetectionRecovered DetectionAction = "recovered"
	// DetectionBreaching: the error rate is high again after recovering.
	DetectionBreaching DetectionAction = "breaching"
)

// DetectionResult is what [Store.DetectIncident] saw and did.
type DetectionResult struct {
	// From and To are the minutes counted: From inclusive, To exclusive.
	From, To     time.Time
	Requests     int64
	ServerErrors int64
	// Breaching reports an error rate above the threshold with at least
	// MinRequests requests; Healthy one at or below it with at least
	// MinRequests requests. Without enough requests, both are false.
	Breaching bool
	Healthy   bool
	Action    DetectionAction
	// Incident is the open automatic incident after detection, if any, and
	// Update the timeline update detection added.
	Incident *Incident
	Update   *IncidentUpdate
}

// ErrorRate returns the share of requests that were server errors.
func (r DetectionResult) ErrorRate() float64 {
	return Stats{Requests: r.Requests, ServerErrors: r.ServerErrors}.ErrorRate()
}

// detectionLock is the transaction advisory lock key serialising detection
// across instances.
const detectionLock = 0x67626f6264657465 // "gbobdete"

const lockDetectionSQL = `SELECT pg_advisory_xact_lock($1)`

const selectTotalsSQL = `
	SELECT COALESCE(sum(requests), 0)::bigint, COALESCE(sum(server_errors), 0)::bigint
	FROM observability_minutes WHERE minute >= $1 AND minute < $2`

const selectOpenAutomaticSQL = `SELECT ` + incidentColumns + `, (SELECT count(*) FROM incident_updates u WHERE u.incident_id = incidents.id)
	FROM incidents WHERE source = 'automatic' AND status <> 'resolved' FOR UPDATE`

// DetectIncident compares every instance's server error rate over the last
// Window, up to and including the current minute, with the threshold:
//
//   - above it, with no automatic incident open: it opens one;
//   - above it again after recovering: it adds a "breaching" update;
//   - at or below it while an automatic incident is open and not yet seen
//     recovered: it adds a "recovered" update, leaving the incident open
//     for operators to resolve.
//
// Windows with fewer than MinRequests requests change nothing. Detection
// runs in one transaction holding an advisory lock, and a unique index
// allows one open automatic incident, so instances running it at the same
// time never open two. It counts incidents.detections by action.
func (s *Store) DetectIncident(ctx context.Context, d Detection) (DetectionResult, error) {
	if d.Severity == "" {
		d.Severity = SeveritySev2
	}
	switch {
	case d.Window < time.Minute || d.Window > time.Hour:
		return DetectionResult{}, errors.New("observability: detection window must be between 1 minute and 1 hour")
	case d.Threshold < 0 || d.Threshold > 1 || d.MinRequests < 1:
		return DetectionResult{}, errors.New("observability: detection threshold must be from 0 to 1, and min requests at least 1")
	case d.Actor.Kind == "":
		return DetectionResult{}, errors.New("observability: detection needs an actor")
	}
	now := s.now().UTC()
	res := DetectionResult{To: now.Truncate(time.Minute).Add(time.Minute)}
	res.From = res.To.Add(-d.Window.Truncate(time.Minute))

	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockDetectionSQL, int64(detectionLock)); err != nil {
			return dbError("lock detection", err)
		}
		if err := tx.QueryRow(ctx, selectTotalsSQL, res.From, res.To).Scan(&res.Requests, &res.ServerErrors); err != nil {
			return dbError("count requests", err)
		}
		enough := res.Requests >= d.MinRequests
		res.Breaching = enough && res.ErrorRate() > d.Threshold
		res.Healthy = enough && !res.Breaching

		var (
			open    Incident
			updates int
		)
		err := tx.QueryRow(ctx, selectOpenAutomaticSQL).Scan(append(open.fields(), &updates)...)
		switch {
		case postgres.IsNoRows(err):
			if res.Breaching {
				return s.openAutomatic(ctx, tx, d, &res, now)
			}
			return nil
		case err != nil:
			return dbError("find open automatic incident", err)
		}
		open = open.utc()
		res.Incident = &open

		var kind UpdateKind
		switch {
		case res.Breaching && open.RecoveredAt != nil:
			open.RecoveredAt, kind, res.Action = nil, UpdateBreaching, DetectionBreaching
		case res.Healthy && open.RecoveredAt == nil:
			open.RecoveredAt, kind, res.Action = &now, UpdateRecovered, DetectionRecovered
		default:
			return nil
		}
		stored, err := storeIncident(ctx, tx, open, now)
		if err != nil {
			return err
		}
		res.Incident = &stored
		if updates >= MaxIncidentUpdates {
			return nil // the incident's state still changes, without a timeline entry
		}
		upd, err := insertUpdate(ctx, tx, stored, kind, detectionMessage(kind, d, res), d.Actor, now)
		if err != nil {
			return err
		}
		res.Update = &upd
		return nil
	})
	if err != nil {
		return DetectionResult{}, err
	}
	if res.Action != DetectionNone {
		s.detections.Add(ctx, 1, metric.WithAttributes(attribute.String("action", string(res.Action))))
	}
	return res, nil
}

func (s *Store) openAutomatic(ctx context.Context, tx pgx.Tx, d Detection, res *DetectionResult, now time.Time) error {
	n := NewIncident{
		Title: fmt.Sprintf("Error rate above %s", percent(d.Threshold)),
		Summary: fmt.Sprintf("Opened automatically: %s of %d requests were server errors over the last %s, above the %s threshold.",
			percent(res.ErrorRate()), res.Requests, d.Window, percent(d.Threshold)),
		Severity: d.Severity, Status: StatusInvestigating, StartedAt: res.From,
		Message: detectionMessage(UpdateOpened, d, *res),
	}
	inc, upd, err := openIncident(ctx, tx, n, SourceAutomatic, d.Actor, now)
	if postgres.IsNoRows(err) {
		return nil // opened by another instance since; the lock makes this unlikely
	}
	if err != nil {
		return err
	}
	res.Action, res.Incident, res.Update = DetectionOpened, &inc, &upd
	return nil
}

func detectionMessage(kind UpdateKind, d Detection, r DetectionResult) string {
	rate := fmt.Sprintf("%s of %d requests over the last %s", percent(r.ErrorRate()), r.Requests, d.Window)
	switch kind {
	case UpdateRecovered:
		return "Error rate recovered: " + rate + ", at or below the " + percent(d.Threshold) + " threshold. Resolve the incident when it is over."
	case UpdateBreaching:
		return "Error rate high again: " + rate + ", above the " + percent(d.Threshold) + " threshold."
	default:
		return "Error rate " + rate + ", above the " + percent(d.Threshold) + " threshold."
	}
}

func percent(f float64) string {
	return fmt.Sprintf("%.1f%%", f*100)
}
