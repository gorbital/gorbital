package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/openapi"

	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// IncidentResponse is an incident.
type IncidentResponse struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title" example:"Checkout requests failing"`
	Summary     string     `json:"summary,omitempty" example:"Payments time out for most customers."`
	Severity    string     `json:"severity" enum:"sev1,sev2,sev3,sev4" doc:"sev1 is the most severe"`
	Status      string     `json:"status" enum:"investigating,identified,monitoring,resolved"`
	Source      string     `json:"source" enum:"manual,automatic" doc:"automatic: opened by the incidents_detect job when the error rate crossed incidents.error_rate_threshold"`
	StartedAt   time.Time  `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	RecoveredAt *time.Time `json:"recovered_at,omitempty" doc:"Automatic incidents: set while detection sees the error rate recovered"`
	CreatedBy   ActorRef   `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ActorRef identifies who acted, without personal details.
type ActorRef struct {
	Kind string `json:"kind" enum:"user,service,system,anonymous" example:"user"`
	ID   string `json:"id,omitempty" example:"usr_01J9Z3Q4"`
}

// IncidentUpdateResponse is one entry of an incident's timeline.
type IncidentUpdateResponse struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind" enum:"opened,update,resolved,recovered,breaching" doc:"recovered and breaching are added by detection on automatic incidents"`
	Message   string    `json:"message" example:"The payment provider confirmed an outage."`
	Status    string    `json:"status" enum:"investigating,identified,monitoring,resolved" doc:"The incident's status after the update"`
	Severity  string    `json:"severity" enum:"sev1,sev2,sev3,sev4" doc:"The incident's severity after the update"`
	Actor     ActorRef  `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}

// IncidentDetailResponse is an incident with its timeline, oldest first.
type IncidentDetailResponse struct {
	IncidentResponse
	Updates []IncidentUpdateResponse `json:"updates"`
}

// IncidentPage is a page of incidents, most recently opened first.
type IncidentPage struct {
	Incidents  []IncidentResponse `json:"incidents"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type incidentOutput struct{ Body IncidentDetailResponse }

type incidentPageOutput struct{ Body IncidentPage }

type openIncidentInput struct {
	Body struct {
		_         struct{}   `json:"-" additionalProperties:"true"`
		Title     string     `json:"title" minLength:"1" maxLength:"200" example:"Checkout requests failing"`
		Summary   string     `json:"summary,omitempty" maxLength:"5000" doc:"What is affected. Don't include personal data" example:"Payments time out for most customers."`
		Severity  string     `json:"severity" enum:"sev1,sev2,sev3,sev4"`
		Status    string     `json:"status,omitempty" enum:"investigating,identified,monitoring" default:"investigating"`
		StartedAt *time.Time `json:"started_at,omitempty" doc:"When it began, up to 90 days ago; default now"`
		Message   string     `json:"message,omitempty" maxLength:"5000" doc:"The first timeline update; default \"Incident opened.\""`
	}
}

type listIncidentsInput struct {
	Status      string    `query:"status" enum:"open,investigating,identified,monitoring,resolved" doc:"open: every status but resolved"`
	Severity    string    `query:"severity" enum:"sev1,sev2,sev3,sev4"`
	Source      string    `query:"source" enum:"manual,automatic"`
	StartedFrom time.Time `query:"started_from" doc:"Incidents that started at or after this time"`
	StartedTo   time.Time `query:"started_to" doc:"Incidents that started before this time"`
	Limit       int       `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Cursor      string    `query:"cursor" maxLength:"30" doc:"next_cursor from the previous page"`
}

type incidentIDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type addIncidentUpdateInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Message  string   `json:"message" minLength:"1" maxLength:"5000" doc:"What changed. Don't include personal data" example:"The payment provider confirmed an outage."`
		Status   string   `json:"status,omitempty" enum:"investigating,identified,monitoring" doc:"The new status; unchanged when left out. Resolve with POST /ops/incidents/{id}/resolve"`
		Severity string   `json:"severity,omitempty" enum:"sev1,sev2,sev3,sev4" doc:"The new severity; unchanged when left out"`
	}
}

type resolveIncidentInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Message string   `json:"message" minLength:"1" maxLength:"5000" doc:"How it was resolved" example:"The provider recovered; failed payments were retried."`
	}
}

type incidentReportInput struct {
	ID     int64  `path:"id" minimum:"1"`
	Format string `query:"format" enum:"json,markdown" doc:"Default: Markdown when Accept prefers text/markdown, otherwise JSON"`
	Accept string `header:"Accept" doc:"text/markdown for Markdown"`
}

type incidentReportOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

type incidentsHandler struct {
	svc *opsusecase.Service
}

// RegisterIncidents adds the incident operations to api (ADR-0064).
func RegisterIncidents(r *gorbital.Router, svc *opsusecase.Service) {
	h := &incidentsHandler{svc: svc}
	tags := []string{"Ops: incidents"}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden}

	operation.Register(r, huma.Operation{
		OperationID: "ops-open-incident", Method: http.MethodPost, Path: "/ops/incidents",
		Summary:     "Open an incident",
		Description: "Records `ops.incident.opened`. Titles, summaries and messages are for operators: don't include personal data.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusCreated,
		Errors: append([]int{http.StatusUnprocessableEntity}, errs...),
	}, h.open)
	operation.Register(r, huma.Operation{
		OperationID: "ops-list-incidents", Method: http.MethodGet, Path: "/ops/incidents",
		Summary: "List incidents, most recently opened first",
		Tags:    tags, Security: openapi.Bearer, Errors: append([]int{http.StatusBadRequest, http.StatusUnprocessableEntity}, errs...),
	}, h.list)
	operation.Register(r, huma.Operation{
		OperationID: "ops-get-incident", Method: http.MethodGet, Path: "/ops/incidents/{id}",
		Summary: "Get an incident with its timeline",
		Tags:    tags, Security: openapi.Bearer, Errors: append([]int{http.StatusNotFound}, errs...),
	}, h.get)
	operation.Register(r, huma.Operation{
		OperationID: "ops-add-incident-update", Method: http.MethodPost, Path: "/ops/incidents/{id}/updates",
		Summary:     "Add a timeline update",
		Description: "Optionally changes the status and severity. Records `ops.incident.updated`. Resolved incidents can't be updated (409).",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusCreated,
		Errors: append([]int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}, errs...),
	}, h.addUpdate)
	operation.Register(r, huma.Operation{
		OperationID: "ops-resolve-incident", Method: http.MethodPost, Path: "/ops/incidents/{id}/resolve",
		Summary:     "Resolve an incident",
		Description: "Sets the status to resolved and the resolution time to now, with a final timeline update. Records `ops.incident.resolved`.",
		Tags:        tags, Security: openapi.Bearer,
		Errors: append([]int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}, errs...),
	}, h.resolve)

	operation.Register(r, huma.Operation{
		OperationID: "ops-incident-report", Method: http.MethodGet, Path: "/ops/incidents/{id}/report",
		Summary: "Get an incident report",
		Description: "The incident and its timeline, with what happened from 15 minutes before it started until 15 minutes after it was " +
			"resolved (or now), at most 24 hours: requests, error rates and latency across instances (needs `ops.observability.read`), audit " +
			"events without IP addresses, user agents or metadata (`ops.audit.read`, at most 200), and releases started " +
			"(`ops.releases.read`). A section the caller can't read is left out with a note. As JSON, or Markdown with `format=markdown` " +
			"or `Accept: text/markdown`.",
		Tags: tags, Security: openapi.Bearer, Errors: append([]int{http.StatusNotFound}, errs...),
	}, h.report, gorbital.Customize(func(api huma.API, op *huma.Operation) {
		reportSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[IncidentReportResponse](), true, "")
		op.Responses = map[string]*huma.Response{"200": {
			Description: "The report",
			Content: map[string]*huma.MediaType{
				"application/json": {Schema: reportSchema},
				"text/markdown":    {Schema: &huma.Schema{Type: huma.TypeString, Examples: []any{"# Incident 12: Checkout requests failing\n…"}}},
			},
		}}
	}))
}

func (h *incidentsHandler) open(ctx context.Context, in *openIncidentInput) (*incidentOutput, error) {
	n := observability.NewIncident{
		Title: in.Body.Title, Summary: in.Body.Summary, Severity: observability.Severity(in.Body.Severity),
		Status: observability.Status(in.Body.Status), Message: in.Body.Message,
	}
	if in.Body.StartedAt != nil {
		n.StartedAt = *in.Body.StartedAt
	}
	view, err := h.svc.OpenIncident(ctx, n)
	if err != nil {
		return nil, err
	}
	return &incidentOutput{Body: incidentDetail(view)}, nil
}

func (h *incidentsHandler) list(ctx context.Context, in *listIncidentsInput) (*incidentPageOutput, error) {
	f := observability.IncidentFilter{
		Severity: observability.Severity(in.Severity), Source: observability.Source(in.Source),
		StartedFrom: in.StartedFrom, StartedTo: in.StartedTo, Limit: in.Limit, Cursor: in.Cursor,
	}
	if in.Status == "open" {
		f.Open = true
	} else {
		f.Status = observability.Status(in.Status)
	}
	page, err := h.svc.ListIncidents(ctx, f)
	if err != nil {
		return nil, err
	}
	out := IncidentPage{Incidents: make([]IncidentResponse, len(page.Incidents)), NextCursor: page.NextCursor}
	for i, inc := range page.Incidents {
		out.Incidents[i] = incidentResponse(inc)
	}
	return &incidentPageOutput{Body: out}, nil
}

func (h *incidentsHandler) get(ctx context.Context, in *incidentIDInput) (*incidentOutput, error) {
	view, err := h.svc.GetIncident(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &incidentOutput{Body: incidentDetail(view)}, nil
}

func (h *incidentsHandler) addUpdate(ctx context.Context, in *addIncidentUpdateInput) (*incidentOutput, error) {
	view, err := h.svc.AddIncidentUpdate(ctx, in.ID, observability.IncidentChange{
		Message: in.Body.Message, Status: observability.Status(in.Body.Status), Severity: observability.Severity(in.Body.Severity),
	})
	if err != nil {
		return nil, err
	}
	return &incidentOutput{Body: incidentDetail(view)}, nil
}

func (h *incidentsHandler) resolve(ctx context.Context, in *resolveIncidentInput) (*incidentOutput, error) {
	view, err := h.svc.ResolveIncident(ctx, in.ID, in.Body.Message)
	if err != nil {
		return nil, err
	}
	return &incidentOutput{Body: incidentDetail(view)}, nil
}

func (h *incidentsHandler) report(ctx context.Context, in *incidentReportInput) (*incidentReportOutput, error) {
	r, err := h.svc.IncidentReport(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	resp := incidentReportResponse(r)
	if in.Format == "markdown" || (in.Format == "" && prefersMarkdown(in.Accept)) {
		return &incidentReportOutput{ContentType: "text/markdown; charset=utf-8", Body: []byte(renderMarkdown(resp))}, nil
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &incidentReportOutput{ContentType: "application/json", Body: body}, nil
}

// prefersMarkdown reports whether an Accept header names text/markdown
// before application/json.
func prefersMarkdown(accept string) bool {
	md, js := strings.Index(accept, "text/markdown"), strings.Index(accept, "application/json")
	return md >= 0 && (js < 0 || md < js)
}

func incidentResponse(inc observability.Incident) IncidentResponse {
	return IncidentResponse{
		ID: inc.ID, Title: inc.Title, Summary: inc.Summary, Severity: string(inc.Severity), Status: string(inc.Status),
		Source: string(inc.Source), StartedAt: inc.StartedAt, ResolvedAt: inc.ResolvedAt, RecoveredAt: inc.RecoveredAt,
		CreatedBy: ActorRef{Kind: string(inc.CreatedByKind), ID: inc.CreatedByID}, CreatedAt: inc.CreatedAt, UpdatedAt: inc.UpdatedAt,
	}
}

func incidentDetail(v opsusecase.IncidentView) IncidentDetailResponse {
	out := IncidentDetailResponse{IncidentResponse: incidentResponse(v.Incident), Updates: make([]IncidentUpdateResponse, len(v.Updates))}
	for i, u := range v.Updates {
		out.Updates[i] = IncidentUpdateResponse{
			ID: u.ID, Kind: string(u.Kind), Message: u.Message, Status: string(u.Status), Severity: string(u.Severity),
			Actor: ActorRef{Kind: string(u.ActorKind), ID: u.ActorID}, CreatedAt: u.CreatedAt,
		}
	}
	return out
}
