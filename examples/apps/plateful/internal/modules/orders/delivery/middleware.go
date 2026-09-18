package delivery

import (
	"net/http"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/settings"
)

// docs:start pause-ordering

// PauseOrdering is the orders module's own middleware, added to the
// customer-facing group with gorbital.Use (routes.go). While an operator has
// turned orders.ordering_paused on in /ops/settings, it answers 503
// ordering_paused to every write on those routes — a kitchen-wide stop for a
// bad deploy or a data migration, without a deploy of its own to turn on and
// another to turn off.
//
// It is middleware rather than a guard because it has nothing to do with who
// is asking: it runs before sign-in and before the guards, so a request that
// would have been refused anyway costs nothing. Reads are left alone, so
// customers can still see the orders they already have.
//
// Retry-After is a promise a client can act on. Half a minute is a guess
// that keeps mobile apps from hammering; a platform that knows how long its
// migration takes should say so.
func PauseOrdering(paused *settings.Setting[bool]) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if safeMethod(r.Method) || paused == nil || !paused.Get(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", "30")
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "ordering_paused",
				"ordering is paused for maintenance; try again shortly"))
		})
	}
}

// safeMethod reports whether the method only reads.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// docs:end pause-ordering
