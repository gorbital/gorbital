package app

import (
	"errors"
	netmail "net/mail"
	"strings"
	"time"

	"apistock.dev/mail"
	authlib "apistock.dev/modules/auth"
	"apistock.dev/modules/settings"
)

const (
	defaultPingMessage   = "pong"
	defaultMailFromEmail = "no-reply@example.com"
)

// appSettings are the runtime settings: non-secret values operators change
// without a redeploy through /ops/settings (ADR-0031). Secrets and
// infrastructure stay in config.go.
type appSettings struct {
	pingMessage   *settings.Setting[string]
	mailFromName  *settings.Setting[string]
	mailFromEmail *settings.Setting[string]
	mailReplyTo   *settings.Setting[string]

	authSessionIdleTTL          *settings.Setting[time.Duration]
	authSessionAbsoluteTTL      *settings.Setting[time.Duration]
	authVerificationCodeTTL     *settings.Setting[time.Duration]
	authResetCodeTTL            *settings.Setting[time.Duration]
	authDeletedAccountRetention *settings.Setting[time.Duration]
}

// mailDefaults fills the sender of every email from the mail.* settings.
func (s appSettings) mailDefaults() mail.Defaults {
	return mail.Defaults{FromName: s.mailFromName, FromEmail: s.mailFromEmail, ReplyTo: s.mailReplyTo}
}

// declareSettings declares every runtime setting.
func declareSettings(reg *settings.Registry) appSettings {
	return appSettings{
		pingMessage: settings.String(reg, "example.ping_message", defaultPingMessage,
			settings.Describe("Reply of GET /v1/ping. An example runtime setting: change it with PUT /ops/settings/example.ping_message."),
			settings.MaxLen(100),
			settings.Validate(notBlank),
		),

		mailFromName: settings.String(reg, "mail.from_name", ServiceName,
			settings.Describe("Sender name on every email, such as Acme."),
			settings.Group("mail"),
			settings.MaxLen(100),
			settings.Validate(func(s string) error {
				if strings.ContainsAny(s, "\r\n") {
					return errors.New("must be a single line")
				}
				return notBlank(s)
			}),
		),
		mailFromEmail: settings.String(reg, "mail.from_email", defaultMailFromEmail,
			settings.Describe("Sender address on every email. With Resend, its domain must be verified in your Resend account."),
			settings.Group("mail"),
			settings.Email(),
		),
		mailReplyTo: settings.String(reg, "mail.reply_to", "",
			settings.Describe("Address replies go to. Empty: replies go to the sender address."),
			settings.Group("mail"),
			settings.MaxLen(254),
			settings.Validate(func(s string) error {
				if s == "" {
					return nil
				}
				if addr, err := netmail.ParseAddress(s); err != nil || addr.Address != s {
					return errors.New("must be empty or an email address such as support@example.com")
				}
				return nil
			}),
		),

		authSessionIdleTTL: settings.Duration(reg, "auth.session_idle_ttl", authlib.DefaultSessionIdleTTL,
			settings.Describe("How long a signed-in session lasts without being used."),
			settings.Range(time.Hour, 90*24*time.Hour),
			settings.ReasonRequired(),
		),
		authSessionAbsoluteTTL: settings.Duration(reg, "auth.session_absolute_ttl", authlib.DefaultSessionAbsoluteTTL,
			settings.Describe("The longest a session lasts, however often it is used; then the user signs in again."),
			settings.Range(24*time.Hour, 365*24*time.Hour),
			settings.ReasonRequired(),
		),
		authVerificationCodeTTL: settings.Duration(reg, "auth.verification_code_ttl", authlib.DefaultVerificationCodeTTL,
			settings.Describe("How long email verification codes stay valid."),
			settings.Range(5*time.Minute, time.Hour),
		),
		authResetCodeTTL: settings.Duration(reg, "auth.reset_code_ttl", authlib.DefaultResetCodeTTL,
			settings.Describe("How long password reset codes stay valid."),
			settings.Range(10*time.Minute, 2*time.Hour),
			settings.ReasonRequired(),
		),
		authDeletedAccountRetention: settings.Duration(reg, "auth.deleted_account_retention", authlib.DefaultDeletedAccountRetention,
			settings.Describe("How long deleted accounts are kept before the auth_cleanup job removes them."),
			settings.Range(24*time.Hour, 365*24*time.Hour),
			settings.ReasonRequired(),
		),
	}
}

func notBlank(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be blank")
	}
	return nil
}
