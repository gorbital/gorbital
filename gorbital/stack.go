package gorbital

import (
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
)

// Stack is the built-in middleware of an app, one field per step, which New
// builds from the configuration (ADR-0083). The fields are in the default
// order, outermost first; [Stack.Default] returns them in that order.
// Change the order, leave steps out or add your own between them with
// [WithStack]:
//
//	gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
//		return []func(http.Handler) http.Handler{
//			s.Recover, s.TrustedProxies, s.RequestID, requireTenantHeader, // yours, early
//			s.Telemetry, s.Observability, s.AccessLog, s.SecureHeaders, s.CORS,
//			s.CrossOrigin, s.BodyLimit, s.Maintenance, s.Auth, s.RateLimit, s.Idempotency,
//		}
//	})
type Stack struct {
	// Recover turns a panic into a 500 problem response (httpx.Recover).
	Recover func(http.Handler) http.Handler
	// TrustedProxies sets the client's address from X-Forwarded-For sent
	// by APP_TRUSTED_PROXIES (httpx.TrustedProxies, ADR-0052).
	TrustedProxies func(http.Handler) http.Handler
	// RequestID gives every request an ID, accepting X-Request-ID only
	// from APP_TRUSTED_CALLERS (httpx.RequestIDFrom).
	RequestID func(http.Handler) http.Handler
	// Telemetry records a span and metrics per request.
	Telemetry func(http.Handler) http.Handler
	// Observability counts requests per route for /ops/observability and
	// automatic incidents (ADR-0064).
	Observability func(http.Handler) http.Handler
	// AccessLog logs one structured line per request (httpx.AccessLog).
	AccessLog func(http.Handler) http.Handler
	// SecureHeaders sets security headers, and HSTS in production
	// (httpx.SecureHeaders).
	SecureHeaders func(http.Handler) http.Handler
	// CORS answers browsers on APP_CORS_ORIGINS (httpx.CORS).
	CORS func(http.Handler) http.Handler
	// CrossOrigin refuses cross-site writes that browsers send with
	// cookies (httpx.CrossOrigin), except the sign-in callbacks other sites
	// post to by design.
	CrossOrigin func(http.Handler) http.Handler
	// BodyLimit refuses bodies over APP_MAX_BODY_BYTES (httpx.BodyLimit).
	BodyLimit func(http.Handler) http.Handler
	// Maintenance answers 503 while the maintenance.enabled setting is on,
	// except health checks, docs, sign-in and /ops (httpx.Maintenance).
	Maintenance func(http.Handler) http.Handler
	// Auth runs the authenticator's middleware ([WithAuth]); without an
	// authenticator it passes requests on unchanged.
	Auth func(http.Handler) http.Handler
	// RateLimit limits requests to /v1/auth/ per client address, by the
	// auth.ip_requests_per_minute setting, shared by every instance.
	RateLimit func(http.Handler) http.Handler
	// Idempotency replays a signed-in POST or PATCH retried with the same
	// Idempotency-Key (ADR-0060).
	Idempotency func(http.Handler) http.Handler
}

// Default returns the steps in the default order, outermost first: the
// order of a v0.1 app's routes.go.
func (s Stack) Default() []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		s.Recover, s.TrustedProxies, s.RequestID, s.Telemetry, s.Observability, s.AccessLog,
		s.SecureHeaders, s.CORS, s.CrossOrigin, s.BodyLimit, s.Maintenance, s.Auth, s.RateLimit, s.Idempotency,
	}
}

// used wraps a step so building the handler records that the step is in
// the chain, which New checks for Recover and Auth after a custom stack.
func used(mw func(http.Handler) http.Handler, flag *atomic.Bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		flag.Store(true)
		return mw(next)
	}
}

// chain wraps h in middleware, the first outermost, skipping nil entries.
func chain(h http.Handler, middleware []func(http.Handler) http.Handler) http.Handler {
	for _, mw := range slices.Backward(middleware) {
		if mw != nil {
			h = mw(h)
		}
	}
	return h
}

// signInPrefix is where the RateLimit step limits requests per client
// address, and what idempotency keys and maintenance mode leave alone: the
// sign-in endpoints answer with session tokens and cookies, which are never
// stored, and staff must be able to sign in during maintenance.
const signInPrefix = "/v1/auth/"

// maintenanceOpen are the routes maintenance mode keeps serving: health
// checks, so load balancers keep instances in rotation; docs; sign-in and
// /ops, so staff can reach the switch (ADR-0051).
var maintenanceOpen = []string{"/livez", "/readyz", "/version", "/openapi.json", "/docs", "/docs/", "/.well-known/", "/ops/", signInPrefix}

// crossSitePosts are the endpoints other sites post to by design: Apple's
// sign-in result (protected by the single-use state and the __Host-oauth
// cookie) and Apple's signed notifications (ADR-0046).
var crossSitePosts = []string{"/v1/auth/apple/callback", "/v1/auth/apple/notifications"}

// exceptCrossSitePosts applies protect to every request but crossSitePosts.
func exceptCrossSitePosts(protect func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		protected := protect(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && slices.Contains(crossSitePosts, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			protected.ServeHTTP(w, r)
		})
	}
}

// signInLimitKey limits changing requests to /v1/auth/, and Google, Apple
// and GitHub sign-in redirects, by client address.
func signInLimitKey(clientKey func(*http.Request) string) func(*http.Request) string {
	return func(r *http.Request) string {
		if !strings.HasPrefix(r.URL.Path, signInPrefix) {
			return ""
		}
		redirect := strings.HasSuffix(r.URL.Path, "/start") || strings.HasSuffix(r.URL.Path, "/callback")
		if r.Method == http.MethodGet && !redirect {
			return ""
		}
		return clientKey(r)
	}
}
