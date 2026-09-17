package app

import (
	"context"
	"log/slog"

	"gorbital.dev/mail"
	"gorbital.dev/modules/mail/smtp"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// Email delivery (ADR-0025, ADR-0037). The provider lives in infra_mail.go,
// managed by `orb add mail`; this file stays the same whichever provider is
// chosen.
const (
	// mailDeliveryDevMail sends every email to orb dev's mail catcher, read
	// in the Dev Portal's Mail screen (ADR-0074). The default in
	// development.
	mailDeliveryDevMail = "devmail"
	// mailDeliveryMailpit sends every email to a Mailpit inbox (an older
	// compose.yaml, or your own), whatever the provider.
	mailDeliveryMailpit = "mailpit"
	// mailDeliveryProvider sends real email through the provider. Always
	// used in production.
	mailDeliveryProvider = "provider"
)

// newMailSender returns the sender the mail worker delivers through.
func newMailSender(cfg Config) (mail.Sender, error) {
	switch cfg.MailDelivery {
	case mailDeliveryDevMail:
		return smtp.New(cfg.DevMailAddr, smtp.WithTLS(smtp.TLSNone))
	case mailDeliveryMailpit:
		return smtp.New(cfg.MailpitAddr, smtp.WithTLS(smtp.TLSNone))
	}
	return newMailProvider(cfg.Mail)
}

// mailInfo describes email delivery for GET /ops/mail.
func mailInfo(cfg Config, s appSettings) opsusecase.MailInfo {
	return opsusecase.MailInfo{
		AppName:  ServiceName,
		Provider: mailProvider,
		Delivery: cfg.MailDelivery,
		Details:  cfg.Mail.details(),
		Sender:   s.mailDefaults(),
	}
}

// warnDefaultSender logs a warning when real email would still be sent from
// the placeholder address.
func warnDefaultSender(ctx context.Context, logger *slog.Logger, cfg Config, s appSettings) {
	if cfg.MailDelivery == mailDeliveryProvider && s.mailFromEmail.Get(ctx) == defaultMailFromEmail {
		logger.WarnContext(ctx, "email is sent from the placeholder address "+defaultMailFromEmail+
			"; set your sender with PUT /ops/settings/mail.from_email")
	}
}
