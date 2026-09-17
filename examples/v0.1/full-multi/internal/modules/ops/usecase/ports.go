package usecase

import (
	"context"
	"encoding/json"

	"gorbital.dev/audit"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/releases"
	"gorbital.dev/modules/settings"
)

// SettingsStore is the runtime settings store. *settings.Store implements it.
type SettingsStore interface {
	List() []settings.View
	Get(key string) (settings.View, error)
	Set(ctx context.Context, key string, value json.RawMessage, change settings.Change) (settings.View, error)
	Reset(ctx context.Context, key string, change settings.Change) (settings.View, error)
	History(ctx context.Context, key string, before int64, limit int) ([]settings.HistoryEntry, error)
	Overrides(ctx context.Context, key, after string, limit int) ([]settings.View, error)
}

// JobsManager manages job definitions, runs and queues. *jobs.Manager
// implements it.
type JobsManager interface {
	Definitions(ctx context.Context) ([]jobs.DefinitionView, error)
	Scheduled(ctx context.Context) ([]jobs.DefinitionView, error)
	Definition(ctx context.Context, name string) (jobs.DefinitionView, error)
	Update(ctx context.Context, name string, patch jobs.ConfigPatch, change jobs.Change) (jobs.DefinitionView, error)
	Reset(ctx context.Context, name string, change jobs.Change) (jobs.DefinitionView, error)
	History(ctx context.Context, name string, before int64, limit int) ([]jobs.DefinitionChange, error)
	RunNow(ctx context.Context, name string) (jobs.JobRun, error)
	Jobs(ctx context.Context, f jobs.JobFilter) (jobs.JobPage, error)
	Job(ctx context.Context, id int64) (jobs.JobRun, error)
	Retry(ctx context.Context, id int64) (jobs.JobRun, error)
	Cancel(ctx context.Context, id int64) (jobs.JobRun, error)
	Queues(ctx context.Context) ([]jobs.Queue, error)
	PauseQueueWithReason(ctx context.Context, name, reason string) error
	ResumeQueueWithReason(ctx context.Context, name, reason string) error
	Overview(ctx context.Context) (jobs.Overview, error)
}

// AuditLog records and lists audit events. *auditpg.Store implements it.
type AuditLog interface {
	audit.Recorder
	List(ctx context.Context, f auditpg.Filter) (auditpg.Page, error)
	Get(ctx context.Context, id int64) (auditpg.StoredEvent, error)
	Stats(ctx context.Context, f auditpg.StatsFilter) (auditpg.Stats, error)
}

// ReleaseLog lists the releases and instances the release tracker records.
// *releases.Store implements it.
type ReleaseLog interface {
	Releases(ctx context.Context, f releases.ReleaseFilter) (releases.ReleasePage, error)
	Current(ctx context.Context) ([]releases.CurrentRelease, error)
	Instances(ctx context.Context, f releases.InstanceFilter) (releases.InstancePage, error)
}

// SuppressionList lists and removes addresses on the email suppression list.
// *suppressionpg.Store implements it.
type SuppressionList interface {
	List(ctx context.Context, f suppressionpg.Filter) (suppressionpg.Page, error)
	Remove(ctx context.Context, id int64) (suppressionpg.Suppression, error)
}

var (
	_ SettingsStore   = (*settings.Store)(nil)
	_ JobsManager     = (*jobs.Manager)(nil)
	_ AuditLog        = (*auditpg.Store)(nil)
	_ ReleaseLog      = (*releases.Store)(nil)
	_ SuppressionList = (*suppressionpg.Store)(nil)
)
