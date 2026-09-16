package delivery

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// IncidentReportResponse is an incident report.
type IncidentReportResponse struct {
	Incident IncidentDetailResponse `json:"incident"`
	From     time.Time              `json:"from" doc:"15 minutes before the incident started"`
	To       time.Time              `json:"to" doc:"15 minutes after it was resolved, or now"`
	// Truncated reports a window cut to 24 hours from its start.
	Truncated bool `json:"truncated" doc:"The window was cut to its first 24 hours"`

	Requests     *ReportRequestsResponse `json:"requests,omitempty" doc:"Requests across instances in the window"`
	RequestsNote string                  `json:"requests_note,omitempty" example:"not included: needs the ops.observability.read permission"`

	AuditEvents          []ReportAuditEventResponse `json:"audit_events" doc:"Newest first, at most 200"`
	AuditEventsTruncated bool                       `json:"audit_events_truncated"`
	AuditEventsNote      string                     `json:"audit_events_note,omitempty"`

	Releases     []ReportReleaseResponse `json:"releases" doc:"Builds instances started in the window"`
	ReleasesNote string                  `json:"releases_note,omitempty"`
}

// ReportRequestsResponse are the requests of a report's window.
type ReportRequestsResponse struct {
	TrafficResponse
	Instances   []InstanceTrafficResponse `json:"instances"`
	ErrorRoutes []RouteTrafficResponse    `json:"error_routes" doc:"Routes with server errors, most first, at most 10"`
	Minutes     []MinuteTrafficResponse   `json:"minutes" doc:"Minutes with requests, oldest first"`
}

// ReportAuditEventResponse is an audit event of a report's window: what
// identifies it, never IP addresses, user agents, labels or metadata. Read
// the full event with GET /ops/audit/{id}.
type ReportAuditEventResponse struct {
	ID           int64     `json:"id"`
	OccurredAt   time.Time `json:"occurred_at"`
	Action       string    `json:"action" example:"settings.value.changed"`
	Outcome      string    `json:"outcome" enum:"success,failure,denied"`
	Actor        ActorRef  `json:"actor"`
	ResourceType string    `json:"resource_type,omitempty" example:"setting"`
	ResourceID   string    `json:"resource_id,omitempty" example:"maintenance.enabled"`
	RequestID    string    `json:"request_id,omitempty"`
}

// ReportReleaseResponse is a build instances started in a report's window.
type ReportReleaseResponse struct {
	Version        string    `json:"version" example:"v1.4.0"`
	Commit         string    `json:"commit,omitempty" example:"3f9a1c2b7d4e8a90"`
	Modified       bool      `json:"modified"`
	FirstStartedAt time.Time `json:"first_started_at"`
	Starts         int       `json:"starts" doc:"Instance starts in the window"`
}

func incidentReportResponse(r opsusecase.IncidentReport) IncidentReportResponse {
	out := IncidentReportResponse{
		Incident: incidentDetail(r.IncidentView), From: r.From, To: r.To, Truncated: r.Truncated,
		RequestsNote: r.RequestsNote, AuditEventsNote: r.AuditEventsNote, AuditEventsTruncated: r.AuditEventsTruncated,
		ReleasesNote: r.ReleasesNote,
		AuditEvents:  make([]ReportAuditEventResponse, len(r.AuditEvents)),
		Releases:     make([]ReportReleaseResponse, len(r.Releases)),
	}
	if sum := r.Requests; sum != nil {
		window := sum.To.Sub(sum.From)
		o := opsusecase.Overview{Window: window, Summary: *sum}
		full := overviewResponse(o)
		errorRoutes := routeResponses(opsusecase.ErrorRoutes(sum.Routes, 10), window)
		out.Requests = &ReportRequestsResponse{
			TrafficResponse: full.TrafficResponse, Instances: full.Instances, ErrorRoutes: errorRoutes, Minutes: full.Minutes,
		}
	}
	for i, e := range r.AuditEvents {
		out.AuditEvents[i] = ReportAuditEventResponse{
			ID: e.ID, OccurredAt: e.OccurredAt, Action: e.Action, Outcome: string(e.Outcome),
			Actor: ActorRef{Kind: string(e.ActorKind), ID: e.ActorID}, ResourceType: e.ResourceType, ResourceID: e.ResourceID, RequestID: e.RequestID,
		}
	}
	for i, rel := range r.Releases {
		out.Releases[i] = ReportReleaseResponse{Version: rel.Version, Commit: rel.Commit, Modified: rel.Modified, FirstStartedAt: rel.FirstStartedAt, Starts: rel.Starts}
	}
	return out
}

// maxMarkdownMinutes bounds the per-minute table of a Markdown report: the
// minutes with the most server errors, in time order.
const maxMarkdownMinutes = 60

// renderMarkdown writes a report as Markdown. Text operators and clients
// wrote (titles, messages, versions, resource IDs) is escaped, so it can't
// add Markdown structure or HTML to pages that render the report.
func renderMarkdown(r IncidentReportResponse) string {
	var b strings.Builder
	inc := r.Incident
	ts := func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }
	fmt.Fprintf(&b, "# Incident %d: %s\n\n", inc.ID, md(inc.Title))
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Severity | %s |\n| Status | %s |\n| Source | %s |\n| Started | %s |\n", inc.Severity, inc.Status, inc.Source, ts(inc.StartedAt))
	if inc.ResolvedAt != nil {
		fmt.Fprintf(&b, "| Resolved | %s |\n| Duration | %s |\n", ts(*inc.ResolvedAt), inc.ResolvedAt.Sub(inc.StartedAt).Round(time.Second))
	}
	fmt.Fprintf(&b, "| Opened by | %s |\n| Report window | %s to %s |\n", actorText(inc.CreatedBy), ts(r.From), ts(r.To))
	if r.Truncated {
		b.WriteString("\nThe report covers the first 24 hours only.\n")
	}
	if inc.Summary != "" {
		fmt.Fprintf(&b, "\n## Summary\n\n%s\n", md(inc.Summary))
	}

	b.WriteString("\n## Timeline\n\n| Time | Update | Status | Severity | By | Message |\n|---|---|---|---|---|---|\n")
	for _, u := range inc.Updates {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", ts(u.CreatedAt), u.Kind, u.Status, u.Severity, actorText(u.Actor), cell(u.Message))
	}

	b.WriteString("\n## Requests\n\n")
	if q := r.Requests; q == nil {
		fmt.Fprintf(&b, "%s.\n", r.RequestsNote)
	} else {
		fmt.Fprintf(&b, "%d requests (%.1f a minute), %d server errors (%.2f%%), %d client errors. Latency p50 %.1f ms, p95 %.1f ms, p99 %.1f ms, max %.1f ms.\n",
			q.Requests, q.RequestsPerMinute, q.ServerErrors, q.ErrorRate*100, q.ClientErrors, q.LatencyMS.P50, q.LatencyMS.P95, q.LatencyMS.P99, q.LatencyMS.Max)
		if len(q.ErrorRoutes) > 0 {
			b.WriteString("\n### Routes with server errors\n\n| Route | Requests | Server errors | Error rate | p95 ms |\n|---|---|---|---|---|\n")
			for _, rt := range q.ErrorRoutes {
				fmt.Fprintf(&b, "| %s %s | %d | %d | %.2f%% | %.1f |\n", rt.Method, cell(routeText(rt.Route)), rt.Requests, rt.ServerErrors, rt.ErrorRate*100, rt.LatencyMS.P95)
			}
		}
		if len(q.Instances) > 0 {
			b.WriteString("\n### Instances\n\n| Instance | Requests | Server errors | p95 ms | Last write |\n|---|---|---|---|---|\n")
			for _, in := range q.Instances {
				fmt.Fprintf(&b, "| %s | %d | %d | %.1f | %s |\n", cell(in.InstanceID), in.Requests, in.ServerErrors, in.LatencyMS.P95, ts(in.LastWrite))
			}
		}
		if minutes := worstMinutes(q.Minutes, maxMarkdownMinutes); len(minutes) > 0 {
			b.WriteString("\n### Minutes\n\n")
			if len(minutes) < len(q.Minutes) {
				fmt.Fprintf(&b, "The %d minutes with the most server errors.\n\n", len(minutes))
			}
			b.WriteString("| Minute | Requests | Server errors | p95 ms |\n|---|---|---|---|\n")
			for _, m := range minutes {
				fmt.Fprintf(&b, "| %s | %d | %d | %.1f |\n", m.Minute.UTC().Format("2006-01-02 15:04"), m.Requests, m.ServerErrors, m.P95MS)
			}
		}
	}

	b.WriteString("\n## Releases\n\n")
	switch {
	case r.ReleasesNote != "":
		fmt.Fprintf(&b, "%s.\n", r.ReleasesNote)
	case len(r.Releases) == 0:
		b.WriteString("No instance started in the window.\n")
	default:
		b.WriteString("| Version | Commit | First started | Starts |\n|---|---|---|---|\n")
		for _, rel := range r.Releases {
			modified := ""
			if rel.Modified {
				modified = " (modified)"
			}
			fmt.Fprintf(&b, "| %s%s | %s | %s | %d |\n", cell(rel.Version), modified, cell(rel.Commit), ts(rel.FirstStartedAt), rel.Starts)
		}
	}

	b.WriteString("\n## Audit events\n\n")
	switch {
	case r.AuditEventsNote != "":
		fmt.Fprintf(&b, "%s.\n", r.AuditEventsNote)
	case len(r.AuditEvents) == 0:
		b.WriteString("None in the window.\n")
	default:
		if r.AuditEventsTruncated {
			fmt.Fprintf(&b, "The newest %d events; list the rest with `GET /ops/audit`.\n\n", len(r.AuditEvents))
		}
		b.WriteString("| Time | Action | Outcome | Actor | Resource | ID |\n|---|---|---|---|---|---|\n")
		for _, e := range r.AuditEvents {
			resource := strings.Trim(e.ResourceType+" "+e.ResourceID, " ")
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d |\n", ts(e.OccurredAt), cell(e.Action), e.Outcome, actorText(e.Actor), cell(resource), e.ID)
		}
	}
	return b.String()
}

// worstMinutes returns up to n minutes with the most server errors, in time
// order.
func worstMinutes(minutes []MinuteTrafficResponse, n int) []MinuteTrafficResponse {
	if len(minutes) <= n {
		return minutes
	}
	worst := slices.Clone(minutes)
	slices.SortStableFunc(worst, func(a, b MinuteTrafficResponse) int { return cmp.Compare(b.ServerErrors, a.ServerErrors) })
	worst = worst[:n]
	slices.SortFunc(worst, func(a, b MinuteTrafficResponse) int { return a.Minute.Compare(b.Minute) })
	return worst
}

func actorText(a ActorRef) string {
	if a.ID == "" {
		return a.Kind
	}
	return a.Kind + " " + cell(a.ID)
}

func routeText(route string) string {
	if route == "" {
		return "(no route)"
	}
	return route
}

// mdEscaper escapes characters that start Markdown structure or HTML.
var mdEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;",
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "#", `\#`, "|", `\|`, "!", `\!`,
)

// md escapes free text for a paragraph; line breaks stay.
func md(s string) string { return mdEscaper.Replace(s) }

// cell escapes free text for a table cell, on one line.
func cell(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(md(s), "\r", " ")), " ")
}
