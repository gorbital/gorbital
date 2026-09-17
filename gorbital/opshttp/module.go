// Package opshttp is the operations API as a gorbital module: the admin
// endpoints under /ops/ for runtime settings, feature flags, background
// jobs, the audit log, releases, email and its suppression list, file
// storage, sign-in methods and rate limits, system health, retention, live
// observability and incidents (ADR-0026, ADR-0051, ADR-0064).
//
// An app adds it in main.go:
//
//	gorbital.Main(
//		gorbital.WithModules(opshttp.Module()),
//		gorbital.WithModules(modules.All()...),
//	)
//
// Its paths, operation IDs, request and response schemas, error codes,
// permissions, roles and audit actions are those of the ops module a v0.1
// app generated, and are public API (ADR-0015). Every operation needs an
// actor holding its ops.* permission; the module declares the roles
// platform_admin (every permission) and ops_viewer (reading). When
// OPS_ALLOWED_IPS is set, requests from other client addresses get 403
// ip_not_allowed (ADR-0085); gorbital.New applies that to every /ops/
// route, sign-in's operator endpoints included, not only to this module's.
//
// The module needs an app built by gorbital.New (or gorbital.Main), which
// gives it the job manager, health checks and what every module declared
// through gorbital.Platform; mounted by hand with gorbital.Mount, it
// registers its routes for the OpenAPI document only.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package opshttp

import (
	"errors"
	"fmt"

	"gorbital.dev/gorbital"

	"gorbital.dev/gorbital/opshttp/internal/delivery"
	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// An Option configures [Module].
type Option func(*options)

type options struct {
	mailProvider string
}

// Mail providers GET /ops/mail reports.
const (
	ProviderResend = "resend"
	ProviderSMTP   = "smtp"
)

// MailProvider sets the email provider GET /ops/mail reports:
// [ProviderResend] (the default), whose API key and webhook secret it
// reports as configured or missing, or [ProviderSMTP]. It doesn't change
// how email is sent: that is gorbital.WithMailer's.
func MailProvider(name string) Option {
	return func(o *options) { o.mailProvider = name }
}

// Module returns the operations API. Its name is "ops".
func Module(opts ...Option) gorbital.Module {
	o := options{mailProvider: ProviderResend}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	var platform *gorbital.Platform
	return gorbital.Module{
		Name:         "ops",
		Errors:       errorMappings(),
		Permissions:  permissions(),
		RateLimiters: []gorbital.RateLimiter{{Name: testEmailLimiter, Keys: "actor ID", Description: "Test emails per operator"}},
		Platform: func(p *gorbital.Platform) error {
			if o.mailProvider != ProviderResend && o.mailProvider != ProviderSMTP {
				return fmt.Errorf("opshttp.MailProvider(%q): the provider must be %s or %s", o.mailProvider, ProviderResend, ProviderSMTP)
			}
			platform = p
			return nil
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			svc := opsusecase.NewService(opsusecase.Deps{})
			if platform != nil {
				deps, err := serviceDeps(d, platform, o)
				if err != nil {
					panic(err) // reported by gorbital.New as an error naming the module
				}
				svc = opsusecase.NewService(deps)
			}
			delivery.RegisterSettings(r, svc)
			delivery.RegisterFlags(r, svc)
			delivery.RegisterJobs(r, svc)
			delivery.RegisterAudit(r, svc)
			delivery.RegisterReleases(r, svc)
			delivery.RegisterMail(r, svc)
			delivery.RegisterStorage(r, svc)
			delivery.RegisterAuth(r, svc)
			delivery.RegisterRateLimits(r, svc)
			delivery.RegisterSystem(r, svc)
			delivery.RegisterAuditStats(r, svc)
			delivery.RegisterJobsOverview(r, svc)
			delivery.RegisterRetention(r, svc)
			delivery.RegisterObservability(r, svc)
			delivery.RegisterIncidents(r, svc)
		},
	}
}

// errNoDatabase reports a module built with a Platform but no database.
var errNoDatabase = errors.New("opshttp: the app has no database pool (gorbital.Deps.DB)")

// Roles the module declares. Role names are public API.
const (
	rolePlatformAdmin = "platform_admin"
	roleOpsViewer     = "ops_viewer"
)

// permissions are the ops.* permissions and the roles that hold them, as a
// v0.1 app's permissions.go declares them. ops.auth.read, which
// /ops/auth/providers and /ops/auth/rate-limits check, and ops.auth.write,
// which rate-limit resets check, are sign-in's: gorbital.dev/gorbital/authhttp
// declares them with the same roles, as it checks them for /ops/auth/users.
// Declared by both modules, they would fail gorbital.New.
func permissions() []gorbital.Permission {
	admin := []string{rolePlatformAdmin}
	both := []string{rolePlatformAdmin, roleOpsViewer}
	return []gorbital.Permission{
		{Name: opsdomain.PermSettingsRead, Description: "Read runtime settings and their history", Roles: both},
		{Name: opsdomain.PermSettingsWrite, Description: "Change and reset runtime settings", Roles: admin},
		{Name: opsdomain.PermFlagsRead, Description: "Read feature flags and their history", Roles: both},
		{Name: opsdomain.PermFlagsWrite, Description: "Change and reset feature flags", Roles: admin},
		{Name: opsdomain.PermJobsRead, Description: "Read job definitions, runs and queues", Roles: both},
		{Name: opsdomain.PermJobsWrite, Description: "Change job configuration; pause and resume queues", Roles: admin},
		{Name: opsdomain.PermJobsRun, Description: "Run, retry and cancel jobs", Roles: admin},
		{Name: opsdomain.PermAuditRead, Description: "Read the audit log", Roles: both},
		{Name: opsdomain.PermReleasesRead, Description: "Read releases and the instances running them", Roles: both},
		{Name: opsdomain.PermMailRead, Description: "See how the app sends email", Roles: both},
		{Name: opsdomain.PermMailTest, Description: "Send a test email", Roles: admin},
		{Name: opsdomain.PermMailWrite, Description: "Remove addresses from the email suppression list", Roles: admin},
		{Name: opsdomain.PermStorageRead, Description: "Browse file storage", Roles: both},
		{Name: opsdomain.PermStorageWrite, Description: "Upload, move and delete files and create signed URLs", Roles: admin},
		{Name: opsdomain.PermSystemRead, Description: "See an instance's health checks, database pool, migrations and runtime", Roles: both},
		{Name: opsdomain.PermObservabilityRead, Description: "See request rates, errors and latency across instances, and stream them", Roles: both},
		{Name: opsdomain.PermIncidentsRead, Description: "Read incidents, their timelines and reports", Roles: both},
		{Name: opsdomain.PermIncidentsWrite, Description: "Open, update and resolve incidents", Roles: admin},
	}
}
