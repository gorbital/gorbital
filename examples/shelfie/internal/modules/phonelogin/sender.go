package phonelogin

import (
	"context"
	"errors"
	"log/slog"
)

// docs:start log-sender

// LogSender writes codes to the log instead of texting them, for
// development: orb dev shows them in its log. Production refuses it; use
// your SMS provider's API behind the Sender interface.
type LogSender struct {
	// Production makes SendCode fail, so a deployment without a real
	// sender answers 503 sms_unavailable instead of logging codes.
	Production bool
}

// SendCode logs the code, or fails in production.
func (s LogSender) SendCode(ctx context.Context, phone, code string) error {
	if s.Production {
		return errors.New("no SMS provider configured")
	}
	slog.WarnContext(ctx, "development text message", "phone", phone, "code", code)
	return nil
}

// docs:end log-sender
