// Package delivery is the partners module's HTTP adapter: the route table in
// this file, and one file per operation.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/webhook"

	"example.com/shelfie/internal/modules/partners/usecase"
)

type handlers struct {
	svc     *usecase.Service
	partner string
}

// docs:start routes

// Register adds the partner routes to r. v verifies every delivery on the
// webhook route, which partner names in what it records.
func Register(r *gorbital.Router, svc *usecase.Service, partner string, v webhook.Verifier) {
	h := handlers{svc: svc, partner: partner}

	// The shop's client gives up after ten seconds, so answering later is
	// only a retry the shop has already scheduled: fail fast instead.
	// Timeout can shorten the app's 30s, never lengthen it.
	hooks := r.Group("/v1/webhooks/partners", gorbital.Tags("Partners"),
		gorbital.Timeout(5*time.Second), guard.Public())
	gorbital.Post(hooks, "/purchases", h.purchase,
		gorbital.Summary("A partner reports a purchase"),
		gorbital.Description("Signed with the partner's secret (Standard Webhooks). Replays and retries of a delivery already recorded answer 200 with the same purchase."),
		gorbital.Errors(http.StatusUnauthorized, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity),
		guard.Webhook(v, guard.WebhookBodyLimit(32<<10)),
		guard.RateLimit(600, time.Minute, guard.ByIP(), guard.Named("partner_webhooks")))

	gorbital.Get(r.Group("/v1/purchases", gorbital.Tags("Partners")), "", h.listPurchases,
		gorbital.Summary("List the books you bought through a partner"),
		guard.Permission(usecase.PermRead))
}

// docs:end routes
