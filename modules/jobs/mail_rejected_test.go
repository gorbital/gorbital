package jobs_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"apistock.dev/mail"
	"apistock.dev/modules/jobs"
)

func TestMailWorkerCancelsRejectedEmail(t *testing.T) {
	pool := newPool(t)
	var calls atomic.Int32
	sender := mail.SenderFunc(func(context.Context, mail.Message) error {
		calls.Add(1)
		return fmt.Errorf("%w: sender domain is not verified", mail.ErrRejected)
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
	waitFor(t, "the rejected email job to be cancelled", func() bool {
		res, err := client.River().JobList(ctx, river.NewJobListParams().Kinds(jobs.MailKind))
		return err == nil && len(res.Jobs) == 1 && res.Jobs[0].State == rivertype.JobStateCancelled
	})
	if n := calls.Load(); n != 1 {
		t.Errorf("sender called %d times, want 1 (a rejected email is never retried)", n)
	}
}
