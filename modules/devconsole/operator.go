package devconsole

import (
	"log/slog"
	"net/http"
	"strings"

	"gorbital.dev/actor"
)

// Operator returns middleware that lets a request under prefix (such as
// "/ops/") act as a, the development operator, when it carries the console
// token as Authorization: Bearer and passes the console's Host, loopback and
// forwarding checks (ADR-0066, ADR-0086). Every other request reaches next
// unchanged, so the app's own authentication still applies to it; a request
// with the token but a wrong Host, a remote peer or forwarding headers (a
// proxy or tunnel such as cloudflared) is logged as refused and continues
// without the operator.
//
// The Dev Portal uses it to call the app's operations APIs in development
// without a signed-in administrator: orb dev adds the token to proxied
// requests, and the app grants a the permissions it chooses, typically its
// platform administrator role's. Audit events record a as the actor. A nil
// console returns middleware that does nothing, so apps can wire it
// unconditionally: without DEV_CONSOLE_TOKEN there is no console, and in
// production the token is refused at startup.
func (c *Console) Operator(prefix string, a actor.Actor, logger *slog.Logger) func(http.Handler) http.Handler {
	if c == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, prefix) || !c.validToken(r.Header.Get("Authorization")) {
				next.ServeHTTP(w, r)
				return
			}
			switch {
			case !allowedHost(r.Host, c.localPort(r)):
				c.logRefusal(r, logger, refusedHost)
			case !loopbackPeer(r.RemoteAddr):
				c.logRefusal(r, logger, refusedPeer)
			case forwarded(r.Header):
				c.logRefusal(r, logger, refusedForwarded)
			default:
				r = r.WithContext(actor.With(r.Context(), a))
			}
			next.ServeHTTP(w, r)
		})
	}
}
