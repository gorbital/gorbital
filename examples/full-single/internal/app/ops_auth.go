package app

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"apistock.dev/actor"
	"apistock.dev/config"
	"apistock.dev/httpx"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// opsAuth protects /ops/* with the OPS_TOKEN bearer token. It is temporary:
// when authentication and platform roles are added, ops routes require a
// signed-in user with ops permissions instead (ADR-0026).
//
// Without OPS_TOKEN the ops routes don't exist. With it, a request carrying
// the token acts as the "ops-token" service actor, so settings and job
// changes are still attributed in history and audit events.
func opsAuth(token config.Secret) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/ops/") {
				next.ServeHTTP(w, r)
				return
			}
			if token.IsZero() {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_found", "no route matches "+r.Method+" "+r.URL.Path))
				return
			}
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token.Reveal())) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="ops"`)
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "a valid ops token is required"))
				return
			}
			ctx := actor.With(r.Context(), actor.Actor{
				Kind:        actor.KindService,
				ID:          "ops-token",
				Label:       "Ops token",
				Permissions: opsdomain.AllPermissions(),
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
