package jobs_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/mail"
	"gorbital.dev/modules/jobs"
)

func TestMailWorkerCancelsRejectedEmail(t *testing.T) {
	pool := newPool(t)
	var calls atomic.Int32
	sender := mail.SenderFunc(func(context.Context, mail.Message) error {
		calls.Add(1)
		return fmt.Errorf("%w: 550 5.1.1 <ada@example.com>: Recipient address rejected", mail.ErrRejected)
	})
	workers := river.NewWorkers()
	if err := jobs.AddMailWorker(workers, sender); err != nil {
		t.Fatal(err)
	}
	client, err := jobs.New(pool, workers, jobs.WithQueues(jobs.DefaultQueues()))
	if err != nil {
		t.Fatal(err)
	}
	run(t, client)

	ctx := context.Background()
	err = jobs.AsyncSender(client).Send(ctx, mail.Message{
		From:    mail.Address{Email: "no-reply@acme.test"},
		To:      []mail.Address{{Email: "ada@example.com"}},
		Subject: "Welcome",
		Text:    "Hello",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	var job *rivertype.JobRow
	waitFor(t, "the rejected email job to be cancelled", func() bool {
		res, err := client.River().JobList(ctx, river.NewJobListParams().Kinds(jobs.MailKind))
		if err != nil || len(res.Jobs) != 1 || res.Jobs[0].State != rivertype.JobStateCancelled {
			return false
		}
		job = res.Jobs[0]
		return true
	})
	// The error is shown in /ops/jobs/runs: the recipient's address is
	// redacted (security review OPS-5).
	if len(job.Errors) != 1 || !strings.Contains(job.Errors[0].Error, "<[email]>: Recipient address rejected") || strings.Contains(job.Errors[0].Error, "ada@example.com") {
		t.Errorf("job errors = %+v, want the provider reply without the address", job.Errors)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("sender called %d times, want 1 (a rejected email is never retried)", n)
	}
}
