package app

import (
	"errors"

	"apistock.dev/config"
	"apistock.dev/mail"
	"apistock.dev/modules/mail/resend"
)

// mailProvider is the email provider, chosen with `aps add mail`. To switch
// provider, run `aps add mail` again: it replaces this file and the provider
// block in .env.example (ADR-0037).
const mailProvider = "resend"

// mailConfig holds the provider's secrets from the environment. The sender
// name, address and reply-to are runtime settings (settings.go), changed in
// /ops/settings without a restart.
type mailConfig struct {
	ResendAPIKey config.Secret // RESEND_API_KEY
}

// loadMailConfig reads the provider's environment variables. required
// reports whether email goes through the provider rather than Mailpit.
func loadMailConfig(_ func(string) string, secret func(string) config.Secret, required bool) (mailConfig, []error) {
	c := mailConfig{ResendAPIKey: secret("RESEND_API_KEY")}
	if required && c.ResendAPIKey.IsZero() {
		return c, []error{errors.New("RESEND_API_KEY is required to send email with Resend: create an API key at https://resend.com/api-keys " +
			"and add it to .env (in development, leave MAIL_DELIVERY empty to use Mailpit instead)")}
	}
	return c, nil
}

// newMailProvider returns the sender that delivers email through Resend.
func newMailProvider(c mailConfig) (mail.Sender, error) {
	return resend.New(c.ResendAPIKey)
}

// details describes the provider's configuration for GET /ops/mail, without
// secrets.
func (c mailConfig) details() map[string]string {
	apiKey := "missing"
	if !c.ResendAPIKey.IsZero() {
		apiKey = "configured"
	}
	return map[string]string{"api_key": apiKey}
}
