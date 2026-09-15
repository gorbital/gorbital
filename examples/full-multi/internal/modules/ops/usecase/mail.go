package usecase

import (
	"context"
	"fmt"
	"maps"
	netmail "net/mail"
	"strings"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/mail"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// MailInfo describes how the app sends email, without secrets.
type MailInfo struct {
	AppName string
	// Provider is the provider chosen with `orb add mail`: resend or smtp.
	Provider string
	// Delivery is mailpit (the development inbox) or provider (real email).
	Delivery string
	// Details are non-secret provider facts, such as whether an API key is
	// configured.
	Details map[string]string
	// Sender holds the runtime settings that fill each email's sender.
	Sender mail.Defaults
}

// MailStatus is how the app sends email right now.
type MailStatus struct {
	Provider  string
	Delivery  string
	Details   map[string]string
	FromName  string
	FromEmail string
	ReplyTo   string
}

// MailStatus returns the provider, delivery mode and current sender.
func (s *Service) MailStatus(ctx context.Context) (MailStatus, error) {
	if err := authorize(ctx, opsdomain.PermMailRead); err != nil {
		return MailStatus{}, err
	}
	st := MailStatus{Provider: s.mail.Provider, Delivery: s.mail.Delivery, Details: maps.Clone(s.mail.Details)}
	if v := s.mail.Sender.FromName; v != nil {
		st.FromName = v.Get(ctx)
	}
	if v := s.mail.Sender.FromEmail; v != nil {
		st.FromEmail = v.Get(ctx)
	}
	if v := s.mail.Sender.ReplyTo; v != nil {
		st.ReplyTo = v.Get(ctx)
	}
	return st, nil
}

// SendTestEmail queues a test email to the address to, through the same
// path as every other email, and records who asked for it.
func (s *Service) SendTestEmail(ctx context.Context, to string) error {
	if err := authorize(ctx, opsdomain.PermMailTest); err != nil {
		return err
	}
	to = strings.TrimSpace(to)
	if addr, err := netmail.ParseAddress(to); err != nil || addr.Address != to {
		return opsdomain.ErrInvalidRecipient
	}
	requester := "an operator"
	if a, ok := actor.From(ctx); ok {
		requester = a.ID
		if a.Label != "" {
			requester = a.Label
		}
	}

	// The recipient is personal data, so the audit event leaves it out.
	err := s.audit.Record(ctx, audit.Event{
		Action:       "mail.test.requested",
		ResourceType: "mail",
		ResourceID:   s.mail.Provider,
		Outcome:      audit.OutcomeSuccess,
		Metadata:     map[string]any{"provider": s.mail.Provider, "delivery": s.mail.Delivery},
	})
	if err != nil {
		return err
	}
	return s.mailer.Send(ctx, mail.Message{
		To:      []mail.Address{{Email: to}},
		Subject: "Test email from " + s.mail.AppName,
		Text: fmt.Sprintf("This is a test email from %s, requested by %s.\n\n"+
			"If you can read it, email delivery works (provider: %s, delivery: %s).\n",
			s.mail.AppName, requester, s.mail.Provider, s.mail.Delivery),
		Tags: map[string]string{"category": "ops_test"},
	})
}
