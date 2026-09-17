package usecase

import (
	"context"
	"strconv"

	"gorbital.dev/audit"
	"gorbital.dev/modules/observability"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// IncidentStore keeps incidents and their timelines. *observability.Store
// implements it.
type IncidentStore interface {
	OpenIncident(ctx context.Context, n observability.NewIncident) (observability.Incident, observability.IncidentUpdate, error)
	UpdateIncident(ctx context.Context, id int64, c observability.IncidentChange) (observability.Incident, observability.IncidentUpdate, error)
	ResolveIncident(ctx context.Context, id int64, message string) (observability.Incident, observability.IncidentUpdate, error)
	Incident(ctx context.Context, id int64) (observability.Incident, error)
	IncidentUpdates(ctx context.Context, id int64) ([]observability.IncidentUpdate, error)
	Incidents(ctx context.Context, f observability.IncidentFilter) (observability.IncidentPage, error)
}

var _ IncidentStore = (*observability.Store)(nil)

// IncidentView is an incident with its timeline, oldest update first.
type IncidentView struct {
	Incident observability.Incident
	Updates  []observability.IncidentUpdate
}

// Audit actions of incidents. They record IDs, status and severity, never
// titles or messages, which operators write freely.
const (
	ActionIncidentOpened   = "ops.incident.opened"
	ActionIncidentUpdated  = "ops.incident.updated"
	ActionIncidentResolved = "ops.incident.resolved"
)

// OpenIncident opens an incident and records ops.incident.opened.
func (s *Service) OpenIncident(ctx context.Context, n observability.NewIncident) (IncidentView, error) {
	if err := authorize(ctx, opsdomain.PermIncidentsWrite); err != nil {
		return IncidentView{}, err
	}
	inc, upd, err := s.incidents.OpenIncident(ctx, n)
	if err != nil {
		return IncidentView{}, err
	}
	if err := s.recordIncident(ctx, ActionIncidentOpened, inc, upd); err != nil {
		return IncidentView{}, err
	}
	return IncidentView{Incident: inc, Updates: []observability.IncidentUpdate{upd}}, nil
}

// ListIncidents lists incidents, most recently opened first.
func (s *Service) ListIncidents(ctx context.Context, f observability.IncidentFilter) (observability.IncidentPage, error) {
	if err := authorize(ctx, opsdomain.PermIncidentsRead); err != nil {
		return observability.IncidentPage{}, err
	}
	return s.incidents.Incidents(ctx, f)
}

// GetIncident returns an incident with its timeline.
func (s *Service) GetIncident(ctx context.Context, id int64) (IncidentView, error) {
	if err := authorize(ctx, opsdomain.PermIncidentsRead); err != nil {
		return IncidentView{}, err
	}
	return s.incidentView(ctx, id)
}

func (s *Service) incidentView(ctx context.Context, id int64) (IncidentView, error) {
	inc, err := s.incidents.Incident(ctx, id)
	if err != nil {
		return IncidentView{}, err
	}
	updates, err := s.incidents.IncidentUpdates(ctx, id)
	if err != nil {
		return IncidentView{}, err
	}
	return IncidentView{Incident: inc, Updates: updates}, nil
}

// AddIncidentUpdate adds a timeline update to an open incident, changing
// its status or severity when given, and records ops.incident.updated.
func (s *Service) AddIncidentUpdate(ctx context.Context, id int64, c observability.IncidentChange) (IncidentView, error) {
	if err := authorize(ctx, opsdomain.PermIncidentsWrite); err != nil {
		return IncidentView{}, err
	}
	inc, upd, err := s.incidents.UpdateIncident(ctx, id, c)
	if err != nil {
		return IncidentView{}, err
	}
	if err := s.recordIncident(ctx, ActionIncidentUpdated, inc, upd); err != nil {
		return IncidentView{}, err
	}
	return s.incidentView(ctx, inc.ID)
}

// ResolveIncident resolves an open incident and records
// ops.incident.resolved.
func (s *Service) ResolveIncident(ctx context.Context, id int64, message string) (IncidentView, error) {
	if err := authorize(ctx, opsdomain.PermIncidentsWrite); err != nil {
		return IncidentView{}, err
	}
	inc, upd, err := s.incidents.ResolveIncident(ctx, id, message)
	if err != nil {
		return IncidentView{}, err
	}
	if err := s.recordIncident(ctx, ActionIncidentResolved, inc, upd); err != nil {
		return IncidentView{}, err
	}
	return s.incidentView(ctx, inc.ID)
}

func (s *Service) recordIncident(ctx context.Context, action string, inc observability.Incident, upd observability.IncidentUpdate) error {
	return s.audit.Record(ctx, IncidentEvent(action, inc, upd))
}

// IncidentEvent is the audit event of a change to an incident, also
// recorded by the incidents_detect job.
func IncidentEvent(action string, inc observability.Incident, upd observability.IncidentUpdate) audit.Event {
	return audit.Event{
		Action:       action,
		ResourceType: "incident",
		ResourceID:   strconv.FormatInt(inc.ID, 10),
		Outcome:      audit.OutcomeSuccess,
		Metadata: map[string]any{
			"source": string(inc.Source), "status": string(inc.Status), "severity": string(inc.Severity),
			"update_id": upd.ID, "update_kind": string(upd.Kind),
		},
	}
}
