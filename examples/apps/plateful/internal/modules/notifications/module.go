// Package notifications tells a restaurant that an order arrived or is
// running late, through a Slack-style incoming webhook it registered itself.
//
// This module is the app extending the framework rather than using it.
// gorbital has no outbound webhooks and no notification channel but email:
// there is no NotificationChannel to implement, no delivery machinery to
// register a transport with, and nothing that knows what a restaurant's
// endpoints are. So the module builds the missing piece — a target policy, a
// hardened HTTP sender, a fanout and a per-endpoint delivery — on top of
// what the framework does give, which is jobs with bounded retries and
// backoff, audit, org-scoped guards and a place for hand-written SQL. None
// of that is re-implemented here; only what is genuinely absent is.
//
// It is in four layers (domain, usecase, repository, delivery) with one file
// per operation, with the sender at the root beside module.go because it is
// an adapter to the outside world rather than a use case.
package notifications

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"

	"example.com/plateful/internal/modules/notifications/delivery"
	"example.com/plateful/internal/modules/notifications/domain"
	"example.com/plateful/internal/modules/notifications/repository"
	"example.com/plateful/internal/modules/notifications/usecase"
)

// options are what [Module] can be built with.
type options struct {
	allowPrivateTargets bool
}

// An Option configures the module.
type Option func(*options)

// AllowPrivateTargets lets endpoints point inside the network this server
// runs in: loopback, private, link-local, unspecified, multicast and
// carrier-grade-NAT addresses, and plain http.
//
// It is for development and for tests. **A production deployment refuses
// private targets, full stop** — there is no setting and no
// per-organisation exception, because an endpoint that could name
// 169.254.169.254 or a database on the private network would be a way for a
// restaurant to make this server fetch things on its behalf. main.go passes:
//
//	notifications.Module(notifications.AllowPrivateTargets(os.Getenv("APP_ENV") != "production"))
func AllowPrivateTargets(allow bool) Option {
	return func(o *options) { o.allowPrivateTargets = allow }
}

// Module returns the notifications module.
//
// It takes arguments, which is why orb gen modules leaves it out of
// modules.gen.go and main.go adds it itself: the generated list calls
// Module() with no arguments, so a module that must be configured declares
// func Module(opts ...Option) and is wired by hand. That is the supported
// way to do this, and it is better than the alternatives — a setting would
// let an operator turn the SSRF guard off at runtime, and reading the
// environment in here would hide a security decision inside a library
// function.
//
// Error codes, permission names, job names and audit action names are public
// API: add new ones, never change existing ones.
func Module(opts ...Option) gorbital.Module {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	// One policy, used twice: the use case refuses a bad URL at registration
	// so the restaurateur gets a 422 they can act on, and the sender refuses
	// the address at dial time, which is the check that actually holds.
	policy := domain.Policy{AllowPrivateTargets: o.allowPrivateTargets}
	sender := NewSender(policy)

	return gorbital.Module{
		Name: "notifications",
		// docs:start notifications-errors
		// guard.OrgMember answers org_not_found for an organisation the caller
		// isn't a member of, and forbidden for a role without the permission.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidEndpoint, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the notification endpoint is not valid"},
			// A URL this deployment won't deliver to gets its own code rather
			// than validation_failed: "your label is too long" and "we will
			// not post to that address" are different conversations, and a
			// client that wants to explain the second one needs to recognise
			// it. The detail says what is wrong in general terms and never
			// quotes the URL back.
			{Err: domain.ErrInvalidURL, Status: http.StatusUnprocessableEntity, Code: "invalid_endpoint_url", Detail: "the endpoint URL must be an https URL that does not point inside a private network"},
			{Err: domain.ErrEndpointNotFound, Status: http.StatusNotFound, Code: "endpoint_not_found", Detail: "the organisation has no notification endpoint with this ID"},
			{Err: domain.ErrEndpointLabelTaken, Status: http.StatusConflict, Code: "endpoint_label_taken", Detail: "the organisation already has a notification endpoint with this label"},
		},
		// docs:end notifications-errors
		// docs:start notifications-permissions
		// Organisation permissions: a member holds them through their role in
		// the restaurant's organisation, an API key only when its scopes
		// include them.
		//
		// Neither is granted to "member", which is the one interesting choice
		// here. Everywhere else in Plateful a member is kitchen staff and can
		// do the day's work; an endpoint is different, because the row behind
		// it is a live credential for the restaurant's own chat. Reading the
		// list tells you which channels exist, and writing it lets you point
		// the restaurant's order alerts at your own webhook. Both belong to
		// the people who run the business.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See where the restaurant's alerts are sent", OrgRoles: []string{"owner", "admin"}},
			{Name: usecase.PermWrite, Description: "Add and remove where the restaurant's alerts are sent", OrgRoles: []string{"owner", "admin"}},
		},
		// docs:end notifications-permissions
		// docs:start notifications-jobs
		// Two jobs, because the split is what makes the retries honest.
		//
		// gorbital.Deps.Jobs is always nil in here — the client is built from
		// these definitions, so it doesn't exist yet — which is why the
		// fanout worker takes no client and finds River's own in the context
		// it is worked with.
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			store := repository.NewStore(d.DB)

			jobs.Define(defs, jobs.Definition[usecase.FanoutArgs]{
				Name:        usecase.FanoutJob,
				Description: "Turns one message for a restaurant into one delivery job per notification endpoint it registered. Enqueued by the app when an order arrives or runs late; nothing runs it on a clock.",
				Worker:      usecase.NewFanoutWorker(store, nil, d.Logger),
				NewArgs:     func() usecase.FanoutArgs { return usecase.FanoutArgs{} },
				Enabled:     true, // no Schedule: on demand only
				Timeout:     15 * time.Second,
				// Three attempts: this one only reads its own database and
				// inserts rows, so a failure is either a blip worth two more
				// tries or a problem more attempts won't fix.
				MaxAttempts: 3,
			})

			jobs.Define(defs, jobs.Definition[usecase.DeliveryArgs]{
				Name:        usecase.DeliveryJob,
				Description: "Posts one message to one notification endpoint. One job per endpoint, so a restaurant's broken channel is retried on its own and the channels that worked are never posted to twice.",
				Worker:      usecase.NewDeliveryWorker(store, sender, d.Audit, d.Logger),
				NewArgs:     func() usecase.DeliveryArgs { return usecase.DeliveryArgs{} },
				Enabled:     true,
				// Longer than the sender's own 10s request timeout, so the
				// sender's deadline is the one that fires and the job's is the
				// backstop.
				Timeout: 15 * time.Second,
				// Five attempts with River's backoff. This is the retry
				// policy, in its entirety: there is no loop and no sleep
				// anywhere in the module. An operator can change this number
				// in /ops without a deploy, and a stuck job is visible in the
				// job list instead of inside a worker's stack.
				MaxAttempts: 5,
			})
		},
		// docs:end notifications-jobs
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger, policy)
			delivery.Register(r, svc)
		},
	}
}
