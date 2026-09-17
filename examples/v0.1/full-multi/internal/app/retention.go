package app

import (
	"context"
	"errors"
	"time"

	"gorbital.dev/modules/jobs"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// retentionPolicy is how long one kind of data is kept and what deletes it,
// for GET /ops/retention (ADR-0051). app.go lists them.
type retentionPolicy struct {
	data      string
	setting   string
	retention func(context.Context) time.Duration
	// job is the job that deletes the data; enforcedBy describes anything
	// else that does.
	job        string
	enforcedBy string
	// oldest reports the oldest row, when it can be read cheaply.
	oldest func(context.Context) (time.Time, bool, error)
}

// retentionReporter builds GET /ops/retention's report.
type retentionReporter struct {
	policies []retentionPolicy
	jobs     *jobs.Manager
}

func (r retentionReporter) RetentionReport(ctx context.Context) ([]opsusecase.RetentionPolicy, error) {
	report := make([]opsusecase.RetentionPolicy, len(r.policies))
	for i, p := range r.policies {
		rp := opsusecase.RetentionPolicy{Data: p.data, Setting: p.setting, Retention: p.retention(ctx), Job: p.job, EnforcedBy: p.enforcedBy}
		if p.oldest != nil {
			oldest, ok, err := p.oldest(ctx)
			if err != nil {
				return nil, err
			}
			if ok {
				rp.OldestAt = &oldest
			}
		}
		if p.job != "" {
			def, err := r.jobs.Definition(ctx, p.job)
			if errors.Is(err, jobs.ErrUnknownDefinition) {
				return nil, errors.New("retention policy " + p.data + " names an undefined job " + p.job)
			} else if err != nil {
				return nil, err
			}
			rp.LastRun, rp.NextRunAt = def.LastRun, def.NextRunAt
		}
		report[i] = rp
	}
	return report, nil
}
