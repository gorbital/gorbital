package mail_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"gorbital.dev/mail"
)

// listOf is a suppression list holding emails.
type listOf []string

func (l listOf) Suppressed(_ context.Context, emails []string) ([]string, error) {
	var out []string
	for _, e := range emails {
		if slices.Contains(l, e) {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestWithSuppressionList(t *testing.T) {
	var sent []mail.Message
	next := mail.SenderFunc(func(_ context.Context, m mail.Message) error {
		sent = append(sent, m)
		return nil
	})
	s := mail.WithSuppressionList(next, listOf{"bounced@example.com"})
	ctx := context.Background()

	m := valid()
	m.To = []mail.Address{{Email: " Bounced@Example.com"}}
	if err := s.Send(ctx, m); !errors.Is(err, mail.ErrSuppressed) || !errors.Is(err, mail.ErrRejected) {
		t.Errorf("Send() to a suppressed address error = %v, want ErrSuppressed wrapping ErrRejected", err)
	}
	if len(sent) != 0 {
		t.Fatalf("a suppressed message was sent: %+v", sent)
	}

	m.To = []mail.Address{{Email: "ada@example.com"}, {Email: "BOUNCED@example.com"}, {Email: "grace@example.com"}}
	if err := s.Send(ctx, m); err != nil {
		t.Fatalf("Send() with some suppressed recipients error = %v", err)
	}
	if len(sent) != 1 || len(sent[0].To) != 2 || sent[0].To[0].Email != "ada@example.com" || sent[0].To[1].Email != "grace@example.com" {
		t.Errorf("sent = %+v, want the two recipients that aren't suppressed", sent)
	}
	if len(m.To) != 3 {
		t.Errorf("the caller's message was changed: %+v", m.To)
	}

	failing := mail.WithSuppressionList(next, mail.SuppressionList(brokenList{}))
	if err := failing.Send(ctx, valid()); err == nil || errors.Is(err, mail.ErrRejected) {
		t.Errorf("Send() with an unreadable list error = %v, want a temporary error", err)
	}
	if len(sent) != 1 {
		t.Errorf("sent without checking the list: %+v", sent)
	}
}

type brokenList struct{}

func (brokenList) Suppressed(context.Context, []string) ([]string, error) {
	return nil, errors.New("database unavailable")
}

func TestNormalizeAddress(t *testing.T) {
	if got := mail.NormalizeAddress("  Ada.Lovelace@Example.COM\n"); got != "ada.lovelace@example.com" {
		t.Errorf("NormalizeAddress() = %q", got)
	}
}
