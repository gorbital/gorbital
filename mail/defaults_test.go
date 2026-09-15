package mail_test

import (
	"context"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/mail"
)

func TestWithDefaults(t *testing.T) {
	var got mail.Message
	next := mail.SenderFunc(func(_ context.Context, m mail.Message) error {
		got = m
		return nil
	})
	defaults := mail.Defaults{
		FromName:  config.Static("Acme"),
		FromEmail: config.Static(" no-reply@acme.test "),
		ReplyTo:   config.Static("support@acme.test"),
	}
	s := mail.WithDefaults(next, defaults)
	ctx := context.Background()

	m := valid()
	m.From, m.ReplyTo = mail.Address{}, nil
	if err := s.Send(ctx, m); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got.From != (mail.Address{Name: "Acme", Email: "no-reply@acme.test"}) || len(got.ReplyTo) != 1 || got.ReplyTo[0].Email != "support@acme.test" {
		t.Errorf("sent From, ReplyTo = %+v, %+v, want the defaults", got.From, got.ReplyTo)
	}

	own := valid()
	own.ReplyTo = []mail.Address{{Email: "team@example.com"}}
	if err := s.Send(ctx, own); err != nil {
		t.Fatal(err)
	}
	if got.From != own.From || got.ReplyTo[0].Email != "team@example.com" {
		t.Errorf("sent From, ReplyTo = %+v, %+v, want the message's own", got.From, got.ReplyTo)
	}

	noReply := mail.WithDefaults(next, mail.Defaults{FromEmail: config.Static("a@acme.test"), ReplyTo: config.Static("")})
	m = valid()
	m.From = mail.Address{}
	if err := noReply.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	if got.From != (mail.Address{Email: "a@acme.test"}) || got.ReplyTo != nil {
		t.Errorf("sent From, ReplyTo = %+v, %+v, want no name and no reply-to", got.From, got.ReplyTo)
	}
}
