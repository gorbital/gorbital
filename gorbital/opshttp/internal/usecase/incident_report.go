package usecase

import (
	"context"
	"slices"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/releases"

	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
)

// Incident report bounds (ADR-0064).
const (
	// ReportMargin is how much before an incident started and after it was
	// resolved its report covers.
	ReportMargin = 15 * time.Minute
	// MaxReportWindow is the longest window a report covers, from its
	// start; request minutes are kept at most 7 days anyway.
	MaxReportWindow = 24 * time.Hour
	// MaxReportAuditEvents and maxReportInstances bound the audit events
	// and release instance starts a report reads.
	MaxReportAuditEvents = 200
	maxReportInstances   = 1000
)

// IncidentReport is what happened during an incident: its timeline, the
// requests, audit events and releases of its window. Each section needs its
// own read permission; a section the reader can't see is left out with a
// note saying why, and so is one that couldn't be read.
type IncidentReport struct {
	IncidentView
	// From and To are the report's window: ReportMargin around the
	// incident, until now while it is open, at most MaxReportWindow
	// (Truncated).
	From, To  time.Time
	Truncated bool

	// Requests is every instance's requests in the window (ops.observability.read).
	Requests     *observability.Summary
	RequestsNote string
	// AuditEvents are the window's audit events, newest first, at most
	// MaxReportAuditEvents (ops.audit.read). The report keeps what
	// identifies them, never IP addresses, user agents, labels or metadata.
	AuditEvents          []auditpg.StoredEvent
	AuditEventsTruncated bool
	AuditEventsNote      string
	// Releases are the builds instances started in the window
	// (ops.releases.read).
	Releases     []ReportRelease
	ReleasesNote string
}

// ReportRelease is a build instances started during a report's window.
type ReportRelease struct {
	Version, Commit string
	Modified        bool
	FirstStartedAt  time.Time
	Starts          int
}

// noteUnavailable explains a report section that couldn't be read.
const noteUnavailable = "not included: couldn't be read"

// IncidentReport builds an incident's report (ops.incidents.read).
func (s *Service) IncidentReport(ctx context.Context, id int64) (IncidentReport, error) {
	if err := authorize(ctx, opsdomain.PermIncidentsRead); err != nil {
		return IncidentReport{}, err
	}
	view, err := s.incidentView(ctx, id)
	if err != nil {
		return IncidentReport{}, err
	}
	now := time.Now().UTC()
	r := IncidentReport{IncidentView: view, From: view.Incident.StartedAt.Add(-ReportMargin).Truncate(time.Minute)}
	end := now
	if resolved := view.Incident.ResolvedAt; resolved != nil && resolved.Add(ReportMargin).Before(now) {
		end = resolved.Add(ReportMargin)
	}
	r.To = end.Truncate(time.Minute).Add(time.Minute)
	if r.To.Sub(r.From) > MaxReportWindow {
		r.To, r.Truncated = r.From.Add(MaxReportWindow), true
	}

	switch {
	case !can(ctx, opsdomain.PermObservabilityRead):
		r.RequestsNote = forbiddenNote(opsdomain.PermObservabilityRead)
	default:
		if sum, err := s.observability.Summary(ctx, r.From, r.To); err != nil {
			r.RequestsNote = noteUnavailable
		} else {
			r.Requests = &sum
		}
	}
	switch {
	case !can(ctx, opsdomain.PermAuditRead):
		r.AuditEventsNote = forbiddenNote(opsdomain.PermAuditRead)
	default:
		if r.AuditEvents, r.AuditEventsTruncated, err = s.reportAudit(ctx, r.From, r.To); err != nil {
			r.AuditEvents, r.AuditEventsNote = nil, noteUnavailable
		}
	}
	switch {
	case !can(ctx, opsdomain.PermReleasesRead):
		r.ReleasesNote = forbiddenNote(opsdomain.PermReleasesRead)
	default:
		if r.Releases, err = s.reportReleases(ctx, r.From, r.To); err != nil {
			r.Releases, r.ReleasesNote = nil, noteUnavailable
		}
	}
	return r, nil
}

func forbiddenNote(permission string) string {
	return "not included: needs the " + permission + " permission"
}

// can reports whether the context's actor holds permission, with any
// second factor it needs.
func can(ctx context.Context, permission string) bool {
	a, ok := actor.From(ctx)
	return ok && a.Can(permission)
}

// reportAudit reads the window's audit events, newest first, without the
// fields a report leaves out.
func (s *Service) reportAudit(ctx context.Context, from, to time.Time) ([]auditpg.StoredEvent, bool, error) {
	var events []auditpg.StoredEvent
	cursor := ""
	for {
		page, err := s.audit.List(ctx, auditpg.Filter{From: from, To: to, Limit: 100, Cursor: cursor})
		if err != nil {
			return nil, false, err
		}
		for _, e := range page.Events {
			if len(events) == MaxReportAuditEvents {
				return events, true, nil
			}
			e.IP, e.UserAgent, e.ActorLabel, e.Metadata, e.TraceID = "", "", "", nil, ""
			events = append(events, e)
		}
		if page.NextCursor == "" {
			return events, false, nil
		}
		cursor = page.NextCursor
	}
}

// reportReleases groups the instance starts in [from, to) by build, reading
// instances newest first until they started before from.
func (s *Service) reportReleases(ctx context.Context, from, to time.Time) ([]ReportRelease, error) {
	var out []ReportRelease
	cursor, read := "", 0
	for read < maxReportInstances {
		page, err := s.releases.Instances(ctx, releases.InstanceFilter{Limit: 100, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, in := range page.Instances {
			read++
			if in.StartedAt.Before(from) {
				return sortReleases(out), nil
			}
			if !in.StartedAt.Before(to) {
				continue
			}
			i := slices.IndexFunc(out, func(r ReportRelease) bool { return r.Version == in.Version && r.Commit == in.Commit })
			if i < 0 {
				out = append(out, ReportRelease{Version: in.Version, Commit: in.Commit})
				i = len(out) - 1
			}
			r := &out[i]
			r.Starts++
			r.Modified = r.Modified || in.Modified
			if r.FirstStartedAt.IsZero() || in.StartedAt.Before(r.FirstStartedAt) {
				r.FirstStartedAt = in.StartedAt
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return sortReleases(out), nil
}

func sortReleases(rs []ReportRelease) []ReportRelease {
	slices.SortFunc(rs, func(a, b ReportRelease) int { return a.FirstStartedAt.Compare(b.FirstStartedAt) })
	return rs
}
