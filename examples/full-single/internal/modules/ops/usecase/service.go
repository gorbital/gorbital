// Package usecase holds the operations module's application logic: every
// operation checks the actor's permission, then calls the settings store,
// jobs manager, audit log, release log or mailer.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/settings"
	"gorbital.dev/ratelimit"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// Deps are the Service's dependencies, wired in internal/app.
type Deps struct {
	Settings SettingsStore
	// Flags is the feature flags store (ADR-0057).
	Flags    FlagsStore
	Jobs     JobsManager
	Audit    AuditLog
	Releases ReleaseLog
	// Mailer queues email; it fills the sender from runtime settings.
	Mailer mail.Sender
	Mail   MailInfo
	// SignInMethods reports which sign-in methods are configured
	// (ADR-0045).
	SignInMethods func() []opsdomain.SignInMethod
	// System describes the instance for GET /ops/system (ADR-0051).
	System SystemReporter
	// Retention lists retention policies for GET /ops/retention (ADR-0051).
	Retention RetentionReporter
	// TestEmailLimiter limits test emails per operator, keyed by actor ID;
	// nil means no limit.
	TestEmailLimiter ratelimit.Taker
	// Suppressions is the email suppression list (ADR-0062).
	Suppressions SuppressionList
	// Observability reads request minutes and Incidents keeps incidents
	// (ADR-0064); *observability.Store implements both.
	Observability ObservabilityStore
	Incidents     IncidentStore
	// Streams bounds the live overview streams of this instance.
	Streams *observability.Streams
	// Reauthenticate returns ctx carrying the actor of a session token, or
	// ErrUnauthenticated when the session ended, for streams to check the
	// session while they run.
	Reauthenticate func(ctx context.Context, token string) (context.Context, error)
	// StreamInterval is how often a stream sends an overview; zero means
	// DefaultStreamInterval.
	StreamInterval time.Duration
}

// TestEmailsPerHour is how many test emails each operator may send an hour.
const TestEmailsPerHour = 5

// Service runs the operations use cases.
type Service struct {
	settings      SettingsStore
	flags         FlagsStore
	jobs          JobsManager
	audit         AuditLog
	releases      ReleaseLog
	mailer        mail.Sender
	mail          MailInfo
	signInMethods func() []opsdomain.SignInMethod
	system        SystemReporter
	retention     RetentionReporter

	testEmailLimiter ratelimit.Taker
	suppressions     SuppressionList

	observability  ObservabilityStore
	incidents      IncidentStore
	streams        *observability.Streams
	reauthenticate func(ctx context.Context, token string) (context.Context, error)
	streamInterval time.Duration
}

// NewService returns a Service.
func NewService(d Deps) *Service {
	return &Service{
		settings: d.Settings, flags: d.Flags, jobs: d.Jobs, audit: d.Audit, releases: d.Releases, mailer: d.Mailer, mail: d.Mail,
		signInMethods: d.SignInMethods, system: d.System, retention: d.Retention,
		testEmailLimiter: d.TestEmailLimiter, suppressions: d.Suppressions,
		observability: d.Observability, incidents: d.Incidents, streams: d.Streams,
		reauthenticate: d.Reauthenticate, streamInterval: d.StreamInterval,
	}
}

// authorize checks the actor's permission. A permission of a role that
// requires two-factor authentication, held by a session without it, returns
// ErrMFARequired, so clients can ask the user to sign in with a second
// factor.
func authorize(ctx context.Context, permission string) error {
	switch err := actor.Require(ctx, permission); {
	case err == nil:
		return nil
	case errors.Is(err, actor.ErrUnauthenticated):
		return opsdomain.ErrUnauthenticated
	case errors.Is(err, actor.ErrStepUpRequired):
		return opsdomain.ErrMFARequired
	default:
		return opsdomain.ErrForbidden
	}
}

// ListSettings returns every runtime setting, or those in group.
func (s *Service) ListSettings(ctx context.Context, group string) ([]settings.View, error) {
	if err := authorize(ctx, opsdomain.PermSettingsRead); err != nil {
		return nil, err
	}
	all := s.settings.List()
	if group == "" {
		return all, nil
	}
	filtered := all[:0]
	for _, v := range all {
		if v.Group == group {
			filtered = append(filtered, v)
		}
	}
	return filtered, nil
}

// GetSetting returns one setting.
func (s *Service) GetSetting(ctx context.Context, key string) (settings.View, error) {
	if err := authorize(ctx, opsdomain.PermSettingsRead); err != nil {
		return settings.View{}, err
	}
	return s.settings.Get(key)
}

// SetSetting changes a setting's value.
func (s *Service) SetSetting(ctx context.Context, key string, value json.RawMessage, change settings.Change) (settings.View, error) {
	if err := authorize(ctx, opsdomain.PermSettingsWrite); err != nil {
		return settings.View{}, err
	}
	return s.settings.Set(ctx, key, value, change)
}

// ResetSetting returns a setting to its default.
func (s *Service) ResetSetting(ctx context.Context, key string, change settings.Change) (settings.View, error) {
	if err := authorize(ctx, opsdomain.PermSettingsWrite); err != nil {
		return settings.View{}, err
	}
	return s.settings.Reset(ctx, key, change)
}

// SettingHistory returns a setting's changes, newest first.
func (s *Service) SettingHistory(ctx context.Context, key string, before int64, limit int) ([]settings.HistoryEntry, error) {
	if err := authorize(ctx, opsdomain.PermSettingsRead); err != nil {
		return nil, err
	}
	return s.settings.History(ctx, key, before, limit)
}

// SettingOverrides returns organisations' own values of a setting, by
// organisation ID after after.
func (s *Service) SettingOverrides(ctx context.Context, key, after string, limit int) ([]settings.View, error) {
	if err := authorize(ctx, opsdomain.PermSettingsRead); err != nil {
		return nil, err
	}
	return s.settings.Overrides(ctx, key, after, limit)
}

// ListJobDefinitions returns every job definition.
func (s *Service) ListJobDefinitions(ctx context.Context) ([]jobs.DefinitionView, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return nil, err
	}
	return s.jobs.Definitions(ctx)
}

// ScheduledJobs returns enabled scheduled jobs, soonest first.
func (s *Service) ScheduledJobs(ctx context.Context) ([]jobs.DefinitionView, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return nil, err
	}
	return s.jobs.Scheduled(ctx)
}

// GetJobDefinition returns one job definition.
func (s *Service) GetJobDefinition(ctx context.Context, name string) (jobs.DefinitionView, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return jobs.DefinitionView{}, err
	}
	return s.jobs.Definition(ctx, name)
}

// UpdateJobDefinition changes a job's configuration.
func (s *Service) UpdateJobDefinition(ctx context.Context, name string, patch jobs.ConfigPatch, change jobs.Change) (jobs.DefinitionView, error) {
	if err := authorize(ctx, opsdomain.PermJobsWrite); err != nil {
		return jobs.DefinitionView{}, err
	}
	return s.jobs.Update(ctx, name, patch, change)
}

// ResetJobDefinition returns a job to its code defaults.
func (s *Service) ResetJobDefinition(ctx context.Context, name string, change jobs.Change) (jobs.DefinitionView, error) {
	if err := authorize(ctx, opsdomain.PermJobsWrite); err != nil {
		return jobs.DefinitionView{}, err
	}
	return s.jobs.Reset(ctx, name, change)
}

// JobDefinitionHistory returns a job definition's changes, newest first.
func (s *Service) JobDefinitionHistory(ctx context.Context, name string, before int64, limit int) ([]jobs.DefinitionChange, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return nil, err
	}
	return s.jobs.History(ctx, name, before, limit)
}

// RunJob enqueues an enabled job now.
func (s *Service) RunJob(ctx context.Context, name string) (jobs.JobRun, error) {
	if err := authorize(ctx, opsdomain.PermJobsRun); err != nil {
		return jobs.JobRun{}, err
	}
	return s.jobs.RunNow(ctx, name)
}

// ListJobRuns lists jobs, newest first.
func (s *Service) ListJobRuns(ctx context.Context, f jobs.JobFilter) (jobs.JobPage, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return jobs.JobPage{}, err
	}
	return s.jobs.Jobs(ctx, f)
}

// GetJobRun returns one job.
func (s *Service) GetJobRun(ctx context.Context, id int64) (jobs.JobRun, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return jobs.JobRun{}, err
	}
	return s.jobs.Job(ctx, id)
}

// RetryJobRun makes a job run again now.
func (s *Service) RetryJobRun(ctx context.Context, id int64) (jobs.JobRun, error) {
	if err := authorize(ctx, opsdomain.PermJobsRun); err != nil {
		return jobs.JobRun{}, err
	}
	return s.jobs.Retry(ctx, id)
}

// CancelJobRun cancels a job.
func (s *Service) CancelJobRun(ctx context.Context, id int64) (jobs.JobRun, error) {
	if err := authorize(ctx, opsdomain.PermJobsRun); err != nil {
		return jobs.JobRun{}, err
	}
	return s.jobs.Cancel(ctx, id)
}

// ListQueues returns active queues.
func (s *Service) ListQueues(ctx context.Context) ([]jobs.Queue, error) {
	if err := authorize(ctx, opsdomain.PermJobsRead); err != nil {
		return nil, err
	}
	return s.jobs.Queues(ctx)
}

// PauseQueue stops workers fetching from a queue. It requires a reason.
func (s *Service) PauseQueue(ctx context.Context, name, reason string) error {
	if err := authorize(ctx, opsdomain.PermJobsWrite); err != nil {
		return err
	}
	return s.jobs.PauseQueueWithReason(ctx, name, reason)
}

// ResumeQueue resumes a paused queue.
func (s *Service) ResumeQueue(ctx context.Context, name, reason string) error {
	if err := authorize(ctx, opsdomain.PermJobsWrite); err != nil {
		return err
	}
	return s.jobs.ResumeQueueWithReason(ctx, name, reason)
}

// ListAuditEvents lists audit events, newest first.
func (s *Service) ListAuditEvents(ctx context.Context, f auditpg.Filter) (auditpg.Page, error) {
	if err := authorize(ctx, opsdomain.PermAuditRead); err != nil {
		return auditpg.Page{}, err
	}
	return s.audit.List(ctx, f)
}

// GetAuditEvent returns one audit event.
func (s *Service) GetAuditEvent(ctx context.Context, id int64) (auditpg.StoredEvent, error) {
	if err := authorize(ctx, opsdomain.PermAuditRead); err != nil {
		return auditpg.StoredEvent{}, err
	}
	return s.audit.Get(ctx, id)
}
