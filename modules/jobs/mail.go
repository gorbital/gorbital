package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"apistock.dev/mail"
)

// MailKind is the job kind that delivers queued email.
const MailKind = "apistock.mail.send"

const (
	mailMaxAttempts = 8
	mailTimeout     = 30 * time.Second
)

// mailArgs is the stored form of a queued email. Its JSON field names are
// fixed so queued jobs survive changes to mail.Message.
type mailArgs struct {
	Message mailMessage `json:"message"`
}

func (mailArgs) Kind() string { return MailKind }

func (mailArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: mailMaxAttempts}
}

type mailAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type mailMessage struct {
	From           mailAddress       `json:"from"`
	To             []mailAddress     `json:"to"`
	ReplyTo        []mailAddress     `json:"reply_to,omitempty"`
	Subject        string            `json:"subject"`
	Text           string            `json:"text,omitempty"`
	HTML           string            `json:"html,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

func fromMail(m mail.Message) mailMessage {
	return mailMessage{
		From:           mailAddress(m.From),
		To:             addresses(m.To, func(a mail.Address) mailAddress { return mailAddress(a) }),
		ReplyTo:        addresses(m.ReplyTo, func(a mail.Address) mailAddress { return mailAddress(a) }),
		Subject:        m.Subject,
		Text:           m.Text,
		HTML:           m.HTML,
		IdempotencyKey: m.IdempotencyKey,
		Tags:           m.Tags,
	}
}

func (m mailMessage) toMail() mail.Message {
	return mail.Message{
		From:           mail.Address(m.From),
		To:             addresses(m.To, func(a mailAddress) mail.Address { return mail.Address(a) }),
		ReplyTo:        addresses(m.ReplyTo, func(a mailAddress) mail.Address { return mail.Address(a) }),
		Subject:        m.Subject,
		Text:           m.Text,
		HTML:           m.HTML,
		IdempotencyKey: m.IdempotencyKey,
		Tags:           m.Tags,
	}
}

func addresses[From, To any](in []From, convert func(From) To) []To {
	if in == nil {
		return nil
	}
	out := make([]To, len(in))
	for i, a := range in {
		out[i] = convert(a)
	}
	return out
}

type mailWorker struct {
	river.WorkerDefaults[mailArgs]
	sender mail.Sender
}

func (w *mailWorker) Work(ctx context.Context, job *river.Job[mailArgs]) error {
	m := job.Args.Message.toMail()
	if err := m.Validate(); err != nil {
		// A stored message that no longer validates will never succeed.
		return river.JobCancel(err)
	}
	if m.IdempotencyKey == "" {
		m.IdempotencyKey = fmt.Sprintf("job-%d", job.ID)
	}
	if err := w.sender.Send(ctx, m); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

func (w *mailWorker) Timeout(*river.Job[mailArgs]) time.Duration { return mailTimeout }

// AddMailWorker registers the worker that delivers queued email through
// sender, such as a Resend or SMTP provider. Register it on the workers of
// the client that works jobs.
func AddMailWorker(workers *river.Workers, sender mail.Sender) error {
	if workers == nil || sender == nil {
		return errors.New("jobs: mail worker needs workers and a sender")
	}
	if err := river.AddWorkerSafely(workers, &mailWorker{sender: sender}); err != nil {
		return fmt.Errorf("jobs: register mail worker: %w", err)
	}
	return nil
}

type asyncSender struct {
	client *Client
}

// AsyncSender returns a [mail.Sender] that validates each message and queues
// it for the mail worker, returning once the job is stored. Delivery is
// retried up to 8 times, and each job's ID becomes the provider idempotency
// key, so a retry never sends twice (ADR-0025).
func AsyncSender(client *Client) mail.Sender {
	return asyncSender{client: client}
}

func (s asyncSender) Send(ctx context.Context, m mail.Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	_, err := s.client.Insert(ctx, mailArgs{Message: fromMail(m)}, nil)
	return err
}
