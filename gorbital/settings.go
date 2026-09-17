package gorbital

import (
	"context"
	"errors"
	"log/slog"
	netmail "net/mail"
	"strings"
	"time"

	"gorbital.dev/mail"
	"gorbital.dev/modules/idempotency"
	"gorbital.dev/modules/releases"
	"gorbital.dev/modules/settings"
)

// defaultMailFromEmail is the placeholder sender New warns about in
// production.
const defaultMailFromEmail = "no-reply@example.com"

// builtinSettings are the runtime settings of what New builds, with the
// keys, defaults and rules of a v0.1 app's internal/app/settings.go, so the
// values operators changed stay in effect after an upgrade (ADR-0031).
// Setting keys are public API. Sign-in's settings (auth.session_*, the
// login limits) belong to the authenticator; auth.ip_requests_per_minute
// stays here because the stack's RateLimit step reads it.
type builtinSettings struct {
	mailFromName  *settings.Setting[string]
	mailFromEmail *settings.Setting[string]
	mailReplyTo   *settings.Setting[string]

	authIPRequestsPerMinute *settings.Setting[int]

	auditRetention            *settings.Setting[time.Duration]
	historyRetention          *settings.Setting[time.Duration]
	releasesInstanceRetention *settings.Setting[time.Duration]
	idempotencyRetention      *settings.Setting[time.Duration]
	observabilityRetention    *settings.Setting[time.Duration]

	incidentsDetectionWindow    *settings.Setting[time.Duration]
	incidentsErrorRateThreshold *settings.Setting[float64]
	incidentsMinRequests        *settings.Setting[int]

	maintenanceEnabled    *settings.Setting[bool]
	maintenanceMessage    *settings.Setting[string]
	maintenanceRetryAfter *settings.Setting[time.Duration]

	logsArchiveEnabled *settings.Setting[bool]
}

// mailDefaults fills the sender of every email from the mail.* settings.
func (s builtinSettings) mailDefaults() mail.Defaults {
	return mail.Defaults{FromName: s.mailFromName, FromEmail: s.mailFromEmail, ReplyTo: s.mailReplyTo}
}

// declareSettings declares the built-in runtime settings; name is the
// app's, the default sender name.
func declareSettings(reg *settings.Registry, name string) builtinSettings {
	return builtinSettings{
		mailFromName: settings.String(reg, "mail.from_name", name,
			settings.Describe("Sender name on every email, such as Acme."),
			settings.Group("mail"),
			settings.MaxLen(100),
			settings.ReasonRequired(),
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
			settings.ReasonRequired(),
		),
		mailReplyTo: settings.String(reg, "mail.reply_to", "",
			settings.Describe("Address replies go to. Empty: replies go to the sender address."),
			settings.Group("mail"),
			settings.MaxLen(254),
			settings.ReasonRequired(),
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

		authIPRequestsPerMinute: settings.Int(reg, "auth.ip_requests_per_minute", 60,
			settings.Describe("Requests to /v1/auth/ allowed per client IP address per minute, across all instances. Behind a load balancer, set APP_TRUSTED_PROXIES so each client has its own budget."),
			settings.Group("rate_limits"),
			settings.Range(10, 10_000),
			settings.ReasonRequired(),
		),

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
		idempotencyRetention: settings.Duration(reg, "idempotency.retention", idempotency.DefaultRetention,
			settings.Describe("How long responses to requests with an Idempotency-Key are kept for retries before the idempotency_cleanup job deletes them. They can hold personal data. Shortening it applies to stored responses at once."),
			settings.Group("retention"),
			settings.Range(time.Hour, 7*24*time.Hour),
			settings.ReasonRequired(),
		),
		observabilityRetention: settings.Duration(reg, "observability.retention", 24*time.Hour,
			settings.Describe("How long request counts per minute, which /ops/observability and incident reports read, are kept before the observability_cleanup job deletes them."),
			settings.Group("retention"),
			settings.Range(time.Hour, 7*24*time.Hour),
			settings.ReasonRequired(),
		),

		incidentsDetectionWindow: settings.Duration(reg, "incidents.detection_window", 5*time.Minute,
			settings.Describe("How far back incidents_detect counts requests, in whole minutes up to the current one."),
			settings.Group("incidents"),
			settings.Range(time.Minute, time.Hour),
			settings.ReasonRequired(),
		),
		incidentsErrorRateThreshold: settings.Float(reg, "incidents.error_rate_threshold", 5,
			settings.Describe("The percentage of requests answered with a server error (5xx), over incidents.detection_window, above which an automatic incident opens."),
			settings.Group("incidents"),
			settings.Range(0.1, 100.0),
			settings.ReasonRequired(),
		),
		incidentsMinRequests: settings.Int(reg, "incidents.min_requests", 100,
			settings.Describe("How many requests incidents.detection_window needs before its error rate opens an incident or counts as recovered."),
			settings.Group("incidents"),
			settings.Range(1, 1_000_000),
			settings.ReasonRequired(),
		),

		maintenanceEnabled: settings.Bool(reg, "maintenance.enabled", false,
			settings.Describe("Answer 503 to every request except health checks, docs, sign-in and /ops."),
			settings.Group("maintenance"),
			settings.ReasonRequired(),
		),
		maintenanceMessage: settings.String(reg, "maintenance.message", "",
			settings.Describe("What clients see while maintenance mode is on. Empty: a generic message."),
			settings.Group("maintenance"),
			settings.MaxLen(500),
		),
		maintenanceRetryAfter: settings.Duration(reg, "maintenance.retry_after", 5*time.Minute,
			settings.Describe("The Retry-After clients get while maintenance mode is on."),
			settings.Group("maintenance"),
			settings.Range(time.Minute, 24*time.Hour),
		),

		logsArchiveEnabled: settings.Bool(reg, "logs.archive.enabled", false,
			settings.Describe("Keep the app's log records in file storage: each instance collects every record it logs into a file for the current hour under LOG_ARCHIVE_DIR and, at the top of the hour or when it shuts down, stores the file gzipped under logs/ in the storage bucket, where /ops/storage lists it. Off: nothing is collected. Log records can hold IDs and addresses; check your retention rules before turning it on."),
			settings.Group("logs"),
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

// warnDefaultSender logs a warning when real email would still be sent from
// the placeholder address.
func warnDefaultSender(ctx context.Context, logger *slog.Logger, cfg Config, s builtinSettings) {
	if cfg.MailDelivery == MailProvider && s.mailFromEmail.Get(ctx) == defaultMailFromEmail {
		logger.WarnContext(ctx, "email is sent from the placeholder address "+defaultMailFromEmail+
			"; set your sender with PUT /ops/settings/mail.from_email")
	}
}
