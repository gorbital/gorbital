package main

import (
	"errors"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/mail"
	"gorbital.dev/modules/mail/resend"
)

// mailProvider is the email provider, chosen with `orb add mail`. To switch
// provider, run `orb add mail` again: it replaces this file and the provider
// block in .env.example (ADR-0037).
const mailProvider = opshttp.ProviderResend

// mailer returns the sender that delivers email through Resend when
// MAIL_DELIVERY is provider, the only mode production allows. In
// development, email goes to orb dev's mail catcher without it. The sender
// name, address and reply-to are runtime settings (mail.*), changed in
// /ops/settings without a restart.
func mailer(cfg gorbital.Config) (mail.Sender, error) {
	if cfg.Mail.ResendAPIKey.IsZero() {
		return nil, errors.New("RESEND_API_KEY is required to send email with Resend: create an API key at https://resend.com/api-keys " +
			"and add it to .env (in development, leave MAIL_DELIVERY empty to use orb dev's mail catcher instead)")
	}
	return resend.New(cfg.Mail.ResendAPIKey)
}
