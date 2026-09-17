package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/config"
)

// DefaultMaintenanceMessage is the problem detail [Maintenance] sends while
// MaintenanceOptions.Message is unset or empty.
const DefaultMaintenanceMessage = "the service is down for maintenance; try again later"

// MaintenanceOptions configures [Maintenance]. The values are read on every
// request, so runtime settings (config.Value) switch maintenance mode on
// every instance without a restart.
type MaintenanceOptions struct {
	// Enabled turns maintenance mode on. Nil never turns it on.
	Enabled config.Value[bool]
	// Message is the problem detail clients see; nil or empty sends
	// DefaultMaintenanceMessage.
	Message config.Value[string]
	// RetryAfter is sent in the Retry-After header, in whole seconds; nil or
	// less than a second sends none.
	RetryAfter config.Value[time.Duration]
	// Open are the paths that keep being served while maintenance mode is
	// on. A path ending in a slash covers everything under it, so "/ops/"
	// keeps /ops/settings open; other paths match exactly. Keep health
	// checks open, or load balancers take every instance out of rotation.
	Open []string
}

// Maintenance answers every request with 503 and the problem code
// maintenance while opts.Enabled is on, except requests for opts.Open paths
// (ADR-0051). It reads the values from opts on each request and does no
// other work, so it costs no database query when the values are runtime
// settings.
func Maintenance(opts MaintenanceOptions) Middleware {
	open := append([]string(nil), opts.Open...)
	return func(next http.Handler) http.Handler {
		if opts.Enabled == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !opts.Enabled.Get(ctx) || maintenanceOpen(open, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			message := ""
			if opts.Message != nil {
				message = opts.Message.Get(ctx)
			}
			if message == "" {
				message = DefaultMaintenanceMessage
			}
			if opts.RetryAfter != nil {
				if seconds := int(opts.RetryAfter.Get(ctx) / time.Second); seconds > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
				}
			}
			WriteProblem(w, r, NewProblem(http.StatusServiceUnavailable, "maintenance", message))
		})
	}
}

// maintenanceOpen reports whether path is one of open, or under an entry
// ending in a slash.
func maintenanceOpen(open []string, path string) bool {
	for _, p := range open {
		if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
			return true
		}
	}
	return false
}
