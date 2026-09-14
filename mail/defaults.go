package mail

import (
	"context"
	"errors"
	"strings"

	"apistock.dev/config"
)

// ErrRejected marks a send that will never succeed as it is, such as an
// invalid message, an unverified sender domain or a refused recipient.
// Providers wrap it; the jobs mail worker stops retrying such sends.
// Temporary failures (network errors, rate limits, provider outages) must not
// wrap it.
var ErrRejected = errors.New("mail: rejected")

// Defaults are live sender values applied to messages that leave them
// empty. Each is read on every send, so runtime settings apply immediately.
// A nil field is ignored.
type Defaults struct {
	FromName  config.Value[string]
	FromEmail config.Value[string]
	// ReplyTo is one email address; an empty value means no reply-to.
	ReplyTo config.Value[string]
}

// WithDefaults returns a [Sender] that fills an empty From and ReplyTo from
// d, then sends through next. Messages that set them keep their own.
func WithDefaults(next Sender, d Defaults) Sender {
	return SenderFunc(func(ctx context.Context, m Message) error {
		if m.From.Email == "" && d.FromEmail != nil {
			m.From.Email = strings.TrimSpace(d.FromEmail.Get(ctx))
			if m.From.Name == "" && d.FromName != nil {
				m.From.Name = strings.TrimSpace(d.FromName.Get(ctx))
			}
		}
		if len(m.ReplyTo) == 0 && d.ReplyTo != nil {
			if email := strings.TrimSpace(d.ReplyTo.Get(ctx)); email != "" {
				m.ReplyTo = []Address{{Email: email}}
			}
		}
		return next.Send(ctx, m)
	})
}
