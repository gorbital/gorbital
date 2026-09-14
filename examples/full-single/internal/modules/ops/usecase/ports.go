package usecase

import (
	"context"
	"encoding/json"

	"apistock.dev/audit"
	"apistock.dev/modules/auditpg"
	"apistock.dev/modules/jobs"
	"apistock.dev/modules/settings"
)

// SettingsStore is the runtime settings store. *settings.Store implements it.
type SettingsStore interface {
	List() []settings.View
	Get(key string) (settings.View, error)
	Set(ctx context.Context, key string, value json.RawMessage, change settings.Change) (settings.View, error)
	Reset(ctx context.Context, key string, change settings.Change) (settings.View, error)
	History(ctx context.Context, key string, before int64, limit int) ([]settings.HistoryEntry, error)
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
	PauseQueue(ctx context.Context, name string) error
	ResumeQueue(ctx context.Context, name string) error
}

// AuditLog records and lists audit events. *auditpg.Store implements it.
type AuditLog interface {
	audit.Recorder
	List(ctx context.Context, f auditpg.Filter) (auditpg.Page, error)
	Get(ctx context.Context, id int64) (auditpg.StoredEvent, error)
}

var (
	_ SettingsStore = (*settings.Store)(nil)
	_ JobsManager   = (*jobs.Manager)(nil)
	_ AuditLog      = (*auditpg.Store)(nil)
)
