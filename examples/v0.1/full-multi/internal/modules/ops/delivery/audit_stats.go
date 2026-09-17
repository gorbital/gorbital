package delivery

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/openapi"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// AuditStatsResponse counts audit events in a window.
type AuditStatsResponse struct {
	From    time.Time         `json:"from"`
	To      time.Time         `json:"to"`
	GroupBy string            `json:"group_by" enum:"action,outcome,actor_kind,resource_type,day"`
	Total   int64             `json:"total"`
	Groups  []AuditStatsGroup `json:"groups" doc:"Largest first, at most 50; every day in order for group_by=day"`
	Other   int64             `json:"other" doc:"Events in groups beyond the first 50"`
}

// AuditStatsGroup is one group's count.
type AuditStatsGroup struct {
	Key   string `json:"key" example:"auth.login.failed" doc:"Empty for events without the grouped field, such as a resource type"`
	Count int64  `json:"count"`
}

type auditStatsOutput struct{ Body AuditStatsResponse }

type auditStatsInput struct {
	GroupBy      string    `query:"group_by" required:"true" enum:"action,outcome,actor_kind,resource_type,day"`
	ActorKind    string    `query:"actor_kind" maxLength:"32" doc:"user, service, system or anonymous"`
	ActorID      string    `query:"actor_id" maxLength:"200"`
	Action       string    `query:"action" maxLength:"200"`
	ActionPrefix string    `query:"action_prefix" maxLength:"200" example:"auth."`
	ResourceType string    `query:"resource_type" maxLength:"100"`
	OrgID        string    `query:"org_id" maxLength:"200"`
	Outcome      string    `query:"outcome" maxLength:"16" doc:"success, failure or denied"`
	From         time.Time `query:"from" doc:"Start, inclusive (RFC 3339); default 7 days before to"`
	To           time.Time `query:"to" doc:"End, exclusive (RFC 3339); default now. At most 90 days after from"`
}

// RegisterAuditStats adds the audit stats operation to api (ADR-0051).
func RegisterAuditStats(api huma.API, svc *opsusecase.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "ops-audit-stats", Method: http.MethodGet, Path: "/ops/audit/stats",
		Summary:     "Count audit events",
		Description: "Counts events in a window of at most 90 days (default the last 7), grouped by action, outcome, actor kind, resource type or UTC day. Filters are the audit list's.",
		Tags:        []string{"Ops: audit"}, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *auditStatsInput) (*auditStatsOutput, error) {
		s, err := svc.AuditStats(ctx, auditpg.StatsFilter{
			GroupBy: auditpg.StatsGroup(in.GroupBy),
			Filter: auditpg.Filter{
				ActorKind: actor.Kind(in.ActorKind), ActorID: in.ActorID, Action: in.Action, ActionPrefix: in.ActionPrefix,
				ResourceType: in.ResourceType, OrgID: in.OrgID, Outcome: audit.Outcome(in.Outcome), From: in.From, To: in.To,
			},
		})
		if err != nil {
			return nil, err
		}
		out := AuditStatsResponse{From: s.From, To: s.To, GroupBy: string(s.GroupBy), Total: s.Total, Other: s.Other, Groups: make([]AuditStatsGroup, len(s.Groups))}
		for i, g := range s.Groups {
			out.Groups[i] = AuditStatsGroup{Key: g.Key, Count: g.Count}
		}
		return &auditStatsOutput{Body: out}, nil
	})
}
