package mail

import (
	"context"
	"fmt"
	"strings"
)

// ErrSuppressed marks a message none of whose recipients may receive email
// because they are on the suppression list: addresses that bounced
// permanently or complained (ADR-0062). It wraps [ErrRejected], so the jobs
// mail worker cancels the send instead of retrying it.
var ErrSuppressed = fmt.Errorf("%w: every recipient is on the suppression list", ErrRejected)

// A SuppressionList holds addresses that must not receive email, such as
// those that bounced permanently or marked a message as spam. Implementations
// must be safe for concurrent use.
type SuppressionList interface {
	// Suppressed returns the addresses among emails, each normalized with
	// [NormalizeAddress], that are on the list. An error means the list
	// couldn't be read.
	Suppressed(ctx context.Context, emails []string) ([]string, error)
}

// NormalizeAddress returns email as suppression lists store it: without
// surrounding spaces and in lower case. Mailbox names are case-sensitive in
// theory but not at any provider in practice, and a list that missed a
// differently capitalized address would keep sending to it.
func NormalizeAddress(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// WithSuppressionList returns a [Sender] that removes suppressed recipients
// from each message before sending it through next. A message left without
// recipients isn't sent and returns [ErrSuppressed]; a list that can't be
// read returns its error, which is temporary, so the send is retried rather
// than delivered to an address that may be suppressed. Reply-to addresses
// aren't checked: they receive nothing.
//
// Wrap the sender the mail worker delivers through, so the check happens
// when the email is sent rather than when it is queued.
func WithSuppressionList(next Sender, list SuppressionList) Sender {
	return SenderFunc(func(ctx context.Context, m Message) error {
		if len(m.To) == 0 {
			return next.Send(ctx, m) // Validate reports it
		}
		emails := make([]string, len(m.To))
		for i, a := range m.To {
			emails[i] = NormalizeAddress(a.Email)
		}
		suppressed, err := list.Suppressed(ctx, emails)
		if err != nil {
			return fmt.Errorf("mail: check the suppression list: %w", err)
		}
		if len(suppressed) == 0 {
			return next.Send(ctx, m)
		}
		skip := make(map[string]bool, len(suppressed))
		for _, email := range suppressed {
			skip[NormalizeAddress(email)] = true
		}
		to := make([]Address, 0, len(m.To))
		for i, a := range m.To {
			if !skip[emails[i]] {
				to = append(to, a)
			}
		}
		if len(to) == 0 {
			return ErrSuppressed
		}
		m.To = to
		return next.Send(ctx, m)
	})
}
