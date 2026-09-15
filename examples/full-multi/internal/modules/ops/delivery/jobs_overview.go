package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/modules/openapi"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// JobsOverviewResponse summarises job work across every instance.
type JobsOverviewResponse struct {
	Queues  []QueueOverviewResponse `json:"queues" doc:"Queues with active workers or unfinished jobs, by name"`
	Failing []JobDefinitionResponse `json:"failing" doc:"Job definitions whose most recent run is retrying or was discarded"`
}

// QueueOverviewResponse counts one queue's unfinished jobs.
type QueueOverviewResponse struct {
	Name             string `json:"name" example:"default"`
	Active           bool   `json:"active" doc:"Some instance runs workers for the queue"`
	Paused           bool   `json:"paused"`
	Available        int64  `json:"available"`
	Scheduled        int64  `json:"scheduled"`
	Running          int64  `json:"running"`
	Retryable        int64  `json:"retryable"`
	DiscardedLastDay int64  `json:"discarded_last_day" doc:"Jobs that ran out of attempts in the last 24 hours"`
}

type jobsOverviewOutput struct{ Body JobsOverviewResponse }

// RegisterJobsOverview adds the jobs overview operation to api (ADR-0051).
func RegisterJobsOverview(api huma.API, svc *opsusecase.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "ops-jobs-overview", Method: http.MethodGet, Path: "/ops/jobs/overview",
		Summary:     "Summarise job work",
		Description: "Unfinished jobs per queue, jobs discarded in the last 24 hours, paused queues, and job definitions whose latest run failed.",
		Tags:        []string{"Ops: job runs"}, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*jobsOverviewOutput, error) {
		o, err := svc.JobsOverview(ctx)
		if err != nil {
			return nil, err
		}
		out := JobsOverviewResponse{Queues: make([]QueueOverviewResponse, len(o.Queues)), Failing: make([]JobDefinitionResponse, len(o.Failing))}
		for i, q := range o.Queues {
			out.Queues[i] = QueueOverviewResponse{
				Name: q.Name, Active: q.Active, Paused: q.Paused, Available: q.Available, Scheduled: q.Scheduled,
				Running: q.Running, Retryable: q.Retryable, DiscardedLastDay: q.DiscardedLastDay,
			}
		}
		for i, d := range o.Failing {
			out.Failing[i] = definitionResponse(d)
		}
		return &jobsOverviewOutput{Body: out}, nil
	})
}
