// Package mail defines email messages and the [Sender] contract implemented
// by provider modules such as mail/resend and mail/smtp (ADR-0025).
//
// Stability: stable (ADR-0015, ADR-0054).
package mail

import (
	"context"
	"errors"
	"fmt"
	netmail "net/mail"
	"regexp"
	"strings"
)

// An Address is an email address with an optional display name.
type Address struct {
	Name  string
	Email string
}

// String formats the address for an email header, quoting the name if needed.
func (a Address) String() string {
	return (&netmail.Address{Name: a.Name, Address: a.Email}).String()
}

// A Message is an email to send. Add fields by setting them; the zero value
// of every optional field means "not set".
type Message struct {
	From    Address
	To      []Address
	ReplyTo []Address
	Subject string
	// Text and HTML are alternative bodies; at least one is required.
	Text string
	HTML string
	// IdempotencyKey lets providers drop duplicate sends, for example when a
	// background job is retried. Jobs use their job ID.
	IdempotencyKey string
	// Tags are provider metadata such as a message category.
	Tags map[string]string
}

// Validate reports whether m has a sender, at least one valid recipient, a
// subject without line breaks and a body.
func (m Message) Validate() error {
	var errs []error
	if err := validAddress(m.From); err != nil {
		errs = append(errs, fmt.Errorf("from: %w", err))
	}
	if len(m.To) == 0 {
		errs = append(errs, errors.New("to: at least one recipient is required"))
	}
	for i, a := range append(append([]Address(nil), m.To...), m.ReplyTo...) {
		if err := validAddress(a); err != nil {
			errs = append(errs, fmt.Errorf("recipient %d: %w", i, err))
		}
	}
	switch {
	case strings.TrimSpace(m.Subject) == "":
		errs = append(errs, errors.New("subject is required"))
	case strings.ContainsAny(m.Subject, "\r\n"):
		errs = append(errs, errors.New("subject must not contain line breaks"))
	}
	if m.Text == "" && m.HTML == "" {
		errs = append(errs, errors.New("text or HTML body is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("mail: invalid message: %w", err)
	}
	return nil
}

func validAddress(a Address) error {
	if strings.ContainsAny(a.Name+a.Email, "\r\n") {
		return errors.New("address must not contain line breaks")
	}
	parsed, err := netmail.ParseAddress(a.Email)
	if err != nil || parsed.Address != a.Email {
		// The address is personal data: errors end up in job runs and logs.
		return errors.New("invalid email address")
	}
	return nil
}

// addressPattern matches text shaped like an email address, including a
// domain literal such as user@[192.0.2.1].
var addressPattern = regexp.MustCompile("[\\p{L}\\p{N}!#$%&'*+/=?^_`{|}~.-]+@(?:[\\p{L}\\p{N}-]+(?:\\.[\\p{L}\\p{N}-]+)*|\\[[^\\]\\s]*\\])")

// RedactedAddress replaces email addresses in text redacted by
// [RedactAddresses].
const RedactedAddress = "[email]"

// RedactAddresses returns text with everything shaped like an email address
// replaced by [RedactedAddress]. Providers and senders apply it to server
// replies before they become errors: SMTP servers and APIs often echo the
// recipient ("550 5.1.1 <jane@example.com>: Recipient address rejected"),
// and errors are shown in job runs and logged, where addresses must not
// appear.
func RedactAddresses(text string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	return addressPattern.ReplaceAllLiteralString(text, RedactedAddress)
}

// A Sender delivers email. Implementations must be safe for concurrent use,
// respect ctx cancellation and return an error when delivery fails.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SenderFunc adapts a function to the [Sender] interface.
type SenderFunc func(ctx context.Context, m Message) error

// Send calls f(ctx, m).
func (f SenderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }
