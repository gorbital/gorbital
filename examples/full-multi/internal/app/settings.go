package app

import (
	"errors"
	netmail "net/mail"
	"net/url"
	"strings"
	"time"

	"apistock.dev/mail"
	authlib "apistock.dev/modules/auth"
	"apistock.dev/modules/releases"
	"apistock.dev/modules/settings"

	orgsusecase "example.com/acme-api/internal/modules/orgs/usecase"
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

	auditRetention            *settings.Setting[time.Duration]
	historyRetention          *settings.Setting[time.Duration]
	releasesInstanceRetention *settings.Setting[time.Duration]

	orgsInvitationURL       *settings.Setting[string]
	orgsInvitationTTL       *settings.Setting[time.Duration]
	orgsDeletedOrgRetention *settings.Setting[time.Duration]
}

// defaultInvitationURL is where invitation links go until the frontend's
// page is set in orgs.invitation_url.
const defaultInvitationURL = "http://localhost:3000/invitations"

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

		// Retention (ADR-0051): GET /ops/retention lists every kind of data
		// and what enforces it.
		auditRetention: settings.Duration(reg, "audit.retention", 365*24*time.Hour,
			settings.Describe("How long audit events are kept before the retention job deletes them. Each deletion leaves a retention.purged audit event."),
			settings.Group("retention"),
			settings.Range(30*24*time.Hour, 10*365*24*time.Hour),
			settings.ReasonRequired(),
		),
		historyRetention: settings.Duration(reg, "ops.history_retention", 365*24*time.Hour,
			settings.Describe("How long the history of runtime setting and job configuration changes is kept before the retention job deletes it."),
			settings.Group("retention"),
			settings.Range(30*24*time.Hour, 10*365*24*time.Hour),
			settings.ReasonRequired(),
		),
		releasesInstanceRetention: settings.Duration(reg, "releases.instance_retention", releases.DefaultRetention,
			settings.Describe("How long instances are listed in /ops/releases after they were last seen. Applied when an instance starts."),
			settings.Group("retention"),
			settings.Range(24*time.Hour, 3*365*24*time.Hour),
		),

		orgsInvitationURL: settings.String(reg, "orgs.invitation_url", defaultInvitationURL,
			settings.Describe("The frontend page invitation emails link to. The link adds #token=… for the page to POST to /v1/invitations/accept."),
			settings.Group("orgs"),
			settings.MaxLen(500),
			settings.Validate(absoluteHTTPURL),
		),
		orgsInvitationTTL: settings.Duration(reg, "orgs.invitation_ttl", orgsusecase.DefaultInvitationTTL,
			settings.Describe("How long an invitation link stays valid."),
			settings.Group("orgs"),
			settings.Range(24*time.Hour, 30*24*time.Hour),
		),
		orgsDeletedOrgRetention: settings.Duration(reg, "orgs.deleted_org_retention", orgsusecase.DefaultDeletedOrgRetention,
			settings.Describe("How long a deleted organisation can be restored before the orgs_purge job removes it with its data."),
			settings.Group("orgs"),
			settings.Range(24*time.Hour, 365*24*time.Hour),
			settings.ReasonRequired(),
		),
	}
}

// absoluteHTTPURL accepts http and https URLs with a host and no fragment.
func absoluteHTTPURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Fragment != "" || strings.ContainsAny(s, " \r\n") {
		return errors.New("must be an http or https URL without a #fragment, such as https://app.example.com/invitations")
	}
	return nil
}

func notBlank(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be blank")
	}
	return nil
}
