package delivery

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/openapi"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// JobConfig is a job definition's configuration.
type JobConfig struct {
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule" doc:"5-field cron in UTC, @every duration, or empty for on-demand" example:"0 3 * * *"`
	Timeout     string `json:"timeout" doc:"Go duration" example:"5m0s"`
	MaxAttempts int    `json:"max_attempts" example:"5"`
	Queue       string `json:"queue" example:"default"`
	Priority    int    `json:"priority" example:"1"`
}

// JobDefinitionResponse is a job definition and its status.
type JobDefinitionResponse struct {
	Name            string          `json:"name" example:"heartbeat"`
	Description     string          `json:"description"`
	Config          JobConfig       `json:"config" doc:"Effective configuration"`
	Defaults        JobConfig       `json:"defaults" doc:"Configuration declared in code"`
	Modified        bool            `json:"modified"`
	InvalidOverride bool            `json:"invalid_override" doc:"The stored override fails validation, so the defaults apply"`
	Version         int64           `json:"version" doc:"Send back when changing the definition"`
	UpdatedAt       *time.Time      `json:"updated_at,omitempty"`
	UpdatedBy       string          `json:"updated_by,omitempty"`
	NextRunAt       *time.Time      `json:"next_run_at,omitempty" doc:"Approximate next scheduled run"`
	LastRun         *JobRunResponse `json:"last_run,omitempty"`
}

// JobDefinitionList is a list of job definitions.
type JobDefinitionList struct {
	Definitions []JobDefinitionResponse `json:"definitions"`
}

// JobDefinitionChange is one change to a job definition.
type JobDefinitionChange struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Action    string         `json:"action" enum:"updated,reset"`
	OldConfig map[string]any `json:"old_config" doc:"Overridden fields before; {} means defaults"`
	NewConfig map[string]any `json:"new_config" doc:"Overridden fields after; {} means defaults"`
	Version   int64          `json:"version"`
	Reason    string         `json:"reason,omitempty"`
	ActorKind string         `json:"actor_kind"`
	ActorID   string         `json:"actor_id"`
	RequestID string         `json:"request_id,omitempty"`
	ChangedAt time.Time      `json:"changed_at"`
}

// JobDefinitionHistory is a page of definition changes, newest first.
type JobDefinitionHistory struct {
	Changes []JobDefinitionChange `json:"changes"`
}

// JobRunError is a failed attempt.
type JobRunError struct {
	At      time.Time `json:"at"`
	Attempt int       `json:"attempt"`
	Message string    `json:"message"`
}

// JobRunResponse is a job run. Arguments are never included.
type JobRunResponse struct {
	ID          int64         `json:"id"`
	Kind        string        `json:"kind" example:"heartbeat"`
	Queue       string        `json:"queue"`
	State       string        `json:"state" example:"completed"`
	Attempt     int           `json:"attempt"`
	MaxAttempts int           `json:"max_attempts"`
	Priority    int           `json:"priority"`
	CreatedAt   time.Time     `json:"created_at"`
	ScheduledAt time.Time     `json:"scheduled_at"`
	AttemptedAt *time.Time    `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time    `json:"finalized_at,omitempty"`
	Errors      []JobRunError `json:"errors,omitempty"`
	RequestID   string        `json:"request_id,omitempty"`
	ActorKind   string        `json:"actor_kind,omitempty"`
	ActorID     string        `json:"actor_id,omitempty"`
}

// JobRunPage is a page of job runs, newest first.
type JobRunPage struct {
	Jobs       []JobRunResponse `json:"jobs"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// QueueResponse is a job queue.
type QueueResponse struct {
	Name      string     `json:"name" example:"default"`
	Paused    bool       `json:"paused"`
	PausedAt  *time.Time `json:"paused_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// QueueList is a list of queues.
type QueueList struct {
	Queues []QueueResponse `json:"queues"`
}

type definitionOutput struct{ Body JobDefinitionResponse }

type definitionListOutput struct{ Body JobDefinitionList }

type definitionHistoryOutput struct{ Body JobDefinitionHistory }

type runOutput struct{ Body JobRunResponse }

type runPageOutput struct{ Body JobRunPage }

type queueListOutput struct{ Body QueueList }

type definitionNameInput struct {
	Name string `path:"name" maxLength:"63" example:"heartbeat"`
}

type updateDefinitionInput struct {
	Name string `path:"name" maxLength:"63" example:"heartbeat"`
	Body struct {
		_           struct{} `json:"-" additionalProperties:"true"`
		Enabled     *bool    `json:"enabled,omitempty"`
		Schedule    *string  `json:"schedule,omitempty" maxLength:"100" doc:"5-field cron in UTC, @every duration, or empty for on-demand"`
		Timeout     *string  `json:"timeout,omitempty" maxLength:"20" doc:"Go duration such as 5m"`
		MaxAttempts *int     `json:"max_attempts,omitempty" minimum:"1" maximum:"100"`
		Queue       *string  `json:"queue,omitempty" maxLength:"100"`
		Priority    *int     `json:"priority,omitempty" minimum:"1" maximum:"4"`
		Version     int64    `json:"version" minimum:"0" doc:"Version last read"`
		Reason      string   `json:"reason,omitempty" maxLength:"500" doc:"Required to disable or reschedule a job"`
	}
}

type resetDefinitionInput struct {
	Name string `path:"name" maxLength:"63" example:"heartbeat"`
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Version int64    `json:"version" minimum:"0"`
		Reason  string   `json:"reason,omitempty" maxLength:"500"`
	}
}

type definitionHistoryInput struct {
	Name   string `path:"name" maxLength:"63" example:"heartbeat"`
	Before int64  `query:"before" minimum:"0"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type listRunsInput struct {
	Kind   string   `query:"kind" maxLength:"63" doc:"Only jobs of this definition or kind"`
	Queue  string   `query:"queue" maxLength:"100"`
	State  []string `query:"state" doc:"Only jobs in these states: available, cancelled, completed, discarded, pending, retryable, running, scheduled"`
	Limit  int      `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Cursor string   `query:"cursor" maxLength:"1000" doc:"next_cursor from the previous page"`
}

type runIDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type queueNameInput struct {
	Name string `path:"name" maxLength:"100" example:"default"`
}

type jobsHandler struct {
	svc *opsusecase.Service
}

// RegisterJobs adds the jobs admin operations to api.
func RegisterJobs(api huma.API, svc *opsusecase.Service) {
	h := &jobsHandler{svc: svc}
	auth := []int{http.StatusUnauthorized, http.StatusForbidden}
	reg := func(tag string, op huma.Operation) huma.Operation {
		op.Tags = []string{tag}
		op.Security = openapi.Bearer
		op.Errors = append(slices.Clone(auth), op.Errors...)
		return op
	}
	const (
		definitions = "Ops: job definitions"
		runs        = "Ops: job runs"
		queues      = "Ops: job queues"
	)

	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-list-job-definitions", Method: http.MethodGet, Path: "/ops/jobs/definitions",
		Summary: "List job definitions",
	}), h.listDefinitions)
	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-list-scheduled-jobs", Method: http.MethodGet, Path: "/ops/jobs/scheduled",
		Summary: "List enabled scheduled jobs, soonest first",
	}), h.scheduled)
	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-get-job-definition", Method: http.MethodGet, Path: "/ops/jobs/definitions/{name}",
		Summary: "Get a job definition", Errors: []int{http.StatusNotFound},
	}), h.getDefinition)
	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-update-job-definition", Method: http.MethodPut, Path: "/ops/jobs/definitions/{name}",
		Summary:     "Change a job's configuration",
		Description: "Send only the fields to change. Applies to every instance within moments. Disabling or rescheduling requires a reason.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.updateDefinition)
	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-reset-job-definition", Method: http.MethodDelete, Path: "/ops/jobs/definitions/{name}",
		Summary: "Reset a job to its code defaults",
		Errors:  []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.resetDefinition)
	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-job-definition-history", Method: http.MethodGet, Path: "/ops/jobs/definitions/{name}/history",
		Summary: "List a job definition's changes", Errors: []int{http.StatusNotFound},
	}), h.definitionHistory)
	huma.Register(api, reg(definitions, huma.Operation{
		OperationID: "ops-run-job", Method: http.MethodPost, Path: "/ops/jobs/definitions/{name}/run",
		Summary: "Run a job now", DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusNotFound, http.StatusConflict},
	}), h.runNow)

	huma.Register(api, reg(runs, huma.Operation{
		OperationID: "ops-list-job-runs", Method: http.MethodGet, Path: "/ops/jobs/runs",
		Summary: "List job runs, newest first", Errors: []int{http.StatusBadRequest, http.StatusUnprocessableEntity},
	}), h.listRuns)
	huma.Register(api, reg(runs, huma.Operation{
		OperationID: "ops-get-job-run", Method: http.MethodGet, Path: "/ops/jobs/runs/{id}",
		Summary: "Get a job run", Errors: []int{http.StatusNotFound},
	}), h.getRun)
	huma.Register(api, reg(runs, huma.Operation{
		OperationID: "ops-retry-job-run", Method: http.MethodPost, Path: "/ops/jobs/runs/{id}/retry",
		Summary: "Retry a job run now", Errors: []int{http.StatusNotFound},
	}), h.retryRun)
	huma.Register(api, reg(runs, huma.Operation{
		OperationID: "ops-cancel-job-run", Method: http.MethodPost, Path: "/ops/jobs/runs/{id}/cancel",
		Summary: "Cancel a job run", Errors: []int{http.StatusNotFound},
	}), h.cancelRun)

	huma.Register(api, reg(queues, huma.Operation{
		OperationID: "ops-list-job-queues", Method: http.MethodGet, Path: "/ops/queues",
		Summary: "List active job queues",
	}), h.listQueues)
	huma.Register(api, reg(queues, huma.Operation{
		OperationID: "ops-pause-job-queue", Method: http.MethodPost, Path: "/ops/queues/{name}/pause",
		Summary: "Pause a queue on every instance", DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusUnprocessableEntity},
	}), h.pauseQueue)
	huma.Register(api, reg(queues, huma.Operation{
		OperationID: "ops-resume-job-queue", Method: http.MethodPost, Path: "/ops/queues/{name}/resume",
		Summary: "Resume a paused queue", DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusUnprocessableEntity},
	}), h.resumeQueue)
}

func (h *jobsHandler) listDefinitions(ctx context.Context, _ *struct{}) (*definitionListOutput, error) {
	views, err := h.svc.ListJobDefinitions(ctx)
	return definitionList(views, err)
}

func (h *jobsHandler) scheduled(ctx context.Context, _ *struct{}) (*definitionListOutput, error) {
	views, err := h.svc.ScheduledJobs(ctx)
	return definitionList(views, err)
}

func definitionList(views []jobs.DefinitionView, err error) (*definitionListOutput, error) {
	if err != nil {
		return nil, err
	}
	out := &definitionListOutput{Body: JobDefinitionList{Definitions: make([]JobDefinitionResponse, len(views))}}
	for i, v := range views {
		out.Body.Definitions[i] = definitionResponse(v)
	}
	return out, nil
}

func (h *jobsHandler) getDefinition(ctx context.Context, in *definitionNameInput) (*definitionOutput, error) {
	v, err := h.svc.GetJobDefinition(ctx, in.Name)
	if err != nil {
		return nil, err
	}
	return &definitionOutput{Body: definitionResponse(v)}, nil
}

func (h *jobsHandler) updateDefinition(ctx context.Context, in *updateDefinitionInput) (*definitionOutput, error) {
	patch := jobs.ConfigPatch{
		Enabled:     in.Body.Enabled,
		Schedule:    in.Body.Schedule,
		MaxAttempts: in.Body.MaxAttempts,
		Queue:       in.Body.Queue,
		Priority:    in.Body.Priority,
	}
	if in.Body.Timeout != nil {
		d, err := time.ParseDuration(*in.Body.Timeout)
		if err != nil {
			return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_job_config", `timeout must be a duration such as "5m"`)
		}
		patch.Timeout = &d
	}
	v, err := h.svc.UpdateJobDefinition(ctx, in.Name, patch, jobs.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, jobError(err)
	}
	return &definitionOutput{Body: definitionResponse(v)}, nil
}

func (h *jobsHandler) resetDefinition(ctx context.Context, in *resetDefinitionInput) (*definitionOutput, error) {
	v, err := h.svc.ResetJobDefinition(ctx, in.Name, jobs.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, jobError(err)
	}
	return &definitionOutput{Body: definitionResponse(v)}, nil
}

func (h *jobsHandler) definitionHistory(ctx context.Context, in *definitionHistoryInput) (*definitionHistoryOutput, error) {
	changes, err := h.svc.JobDefinitionHistory(ctx, in.Name, in.Before, in.Limit)
	if err != nil {
		return nil, err
	}
	out := &definitionHistoryOutput{Body: JobDefinitionHistory{Changes: make([]JobDefinitionChange, len(changes))}}
	for i, c := range changes {
		out.Body.Changes[i] = JobDefinitionChange{
			ID: c.ID, Name: c.Name, Action: c.Action,
			OldConfig: objectJSON(c.OldConfig), NewConfig: objectJSON(c.NewConfig),
			Version: c.Version, Reason: c.Reason, ActorKind: c.ActorKind, ActorID: c.ActorID,
			RequestID: c.RequestID, ChangedAt: c.ChangedAt,
		}
	}
	return out, nil
}

func (h *jobsHandler) runNow(ctx context.Context, in *definitionNameInput) (*runOutput, error) {
	run, err := h.svc.RunJob(ctx, in.Name)
	if err != nil {
		return nil, err
	}
	return &runOutput{Body: runResponse(run)}, nil
}

func (h *jobsHandler) listRuns(ctx context.Context, in *listRunsInput) (*runPageOutput, error) {
	states := make([]rivertype.JobState, 0, len(in.State))
	for _, s := range in.State {
		state := rivertype.JobState(s)
		if !slices.Contains(rivertype.JobStates(), state) {
			return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_job_state",
				"state must be one of available, cancelled, completed, discarded, pending, retryable, running, scheduled")
		}
		states = append(states, state)
	}
	page, err := h.svc.ListJobRuns(ctx, jobs.JobFilter{Kind: in.Kind, Queue: in.Queue, States: states, Limit: in.Limit, Cursor: in.Cursor})
	if err != nil {
		return nil, err
	}
	out := &runPageOutput{Body: JobRunPage{Jobs: make([]JobRunResponse, len(page.Jobs)), NextCursor: page.NextCursor}}
	for i, run := range page.Jobs {
		out.Body.Jobs[i] = runResponse(run)
	}
	return out, nil
}

func (h *jobsHandler) getRun(ctx context.Context, in *runIDInput) (*runOutput, error) {
	run, err := h.svc.GetJobRun(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &runOutput{Body: runResponse(run)}, nil
}

func (h *jobsHandler) retryRun(ctx context.Context, in *runIDInput) (*runOutput, error) {
	run, err := h.svc.RetryJobRun(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &runOutput{Body: runResponse(run)}, nil
}

func (h *jobsHandler) cancelRun(ctx context.Context, in *runIDInput) (*runOutput, error) {
	run, err := h.svc.CancelJobRun(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &runOutput{Body: runResponse(run)}, nil
}

func (h *jobsHandler) listQueues(ctx context.Context, _ *struct{}) (*queueListOutput, error) {
	queues, err := h.svc.ListQueues(ctx)
	if err != nil {
		return nil, err
	}
	out := &queueListOutput{Body: QueueList{Queues: make([]QueueResponse, len(queues))}}
	for i, q := range queues {
		out.Body.Queues[i] = QueueResponse{Name: q.Name, Paused: q.Paused, PausedAt: q.PausedAt, CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt}
	}
	return out, nil
}

func (h *jobsHandler) pauseQueue(ctx context.Context, in *queueNameInput) (*struct{}, error) {
	return nil, h.svc.PauseQueue(ctx, in.Name)
}

func (h *jobsHandler) resumeQueue(ctx context.Context, in *queueNameInput) (*struct{}, error) {
	return nil, h.svc.ResumeQueue(ctx, in.Name)
}

// jobError turns a rejected configuration into a problem carrying the reason.
func jobError(err error) error {
	var invalid *jobs.InvalidConfigError
	if errors.As(err, &invalid) {
		return httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_job_config", invalid.Reason)
	}
	return err
}

func jobConfig(c jobs.Config) JobConfig {
	return JobConfig{
		Enabled: c.Enabled, Schedule: c.Schedule, Timeout: c.Timeout.String(),
		MaxAttempts: c.MaxAttempts, Queue: c.Queue, Priority: c.Priority,
	}
}

func definitionResponse(v jobs.DefinitionView) JobDefinitionResponse {
	r := JobDefinitionResponse{
		Name: v.Name, Description: v.Description,
		Config: jobConfig(v.Config), Defaults: jobConfig(v.Defaults),
		Modified: v.Modified, InvalidOverride: v.InvalidOverride,
		Version: v.Version, UpdatedBy: v.UpdatedBy,
	}
	if !v.UpdatedAt.IsZero() {
		r.UpdatedAt = &v.UpdatedAt
	}
	if !v.NextRunAt.IsZero() {
		r.NextRunAt = &v.NextRunAt
	}
	if v.LastRun != nil {
		last := runResponse(*v.LastRun)
		r.LastRun = &last
	}
	return r
}

func runResponse(run jobs.JobRun) JobRunResponse {
	r := JobRunResponse{
		ID: run.ID, Kind: run.Kind, Queue: run.Queue, State: string(run.State),
		Attempt: run.Attempt, MaxAttempts: run.MaxAttempts, Priority: run.Priority,
		CreatedAt: run.CreatedAt, ScheduledAt: run.ScheduledAt,
		AttemptedAt: run.AttemptedAt, FinalizedAt: run.FinalizedAt,
		RequestID: run.RequestID, ActorKind: run.ActorKind, ActorID: run.ActorID,
	}
	for _, e := range run.Errors {
		r.Errors = append(r.Errors, JobRunError{At: e.At, Attempt: e.Attempt, Message: e.Message})
	}
	return r
}

func objectJSON(raw []byte) map[string]any {
	obj := map[string]any{}
	if v, ok := decodeJSON(raw).(map[string]any); ok {
		obj = v
	}
	return obj
}
