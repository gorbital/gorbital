package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"apistock.dev/actor"
	"apistock.dev/httpx"
	"apistock.dev/modules/settings"
)

// maintenanceOpen are the routes maintenance mode keeps serving: health
// checks, so load balancers keep instances in rotation; docs; sign-in and
// /ops, so staff can reach the switch (ADR-0051). A path ending in / covers
// everything under it.
var maintenanceOpen = []string{"/livez", "/readyz", "/version", "/openapi.json", "/docs", "/docs/", "/.well-known/", "/ops/", "/v1/auth/"}

// defaultMaintenanceMessage is the detail when maintenance.message is empty.
const defaultMaintenanceMessage = "the service is down for maintenance; try again later"

func maintenanceOpenPath(path string) bool {
	for _, open := range maintenanceOpen {
		if path == open || (strings.HasSuffix(open, "/") && strings.HasPrefix(path, open)) {
			return true
		}
	}
	return false
}

// maintenance answers 503 with problem code maintenance while
// maintenance.enabled is on, except on maintenanceOpen routes. Settings are
// read from memory, so it adds no database query per request.
func (a *App) maintenance() httpx.Middleware {
	s := a.appSettings
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !s.maintenanceEnabled.Get(ctx) || maintenanceOpenPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			message := s.maintenanceMessage.Get(ctx)
			if message == "" {
				message = defaultMaintenanceMessage
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(s.maintenanceRetryAfter.Get(ctx)/time.Second)))
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "maintenance", message))
		})
	}
}

// SetMaintenance turns maintenance mode on or off from the command line, for
// when nobody can reach /ops. A message, when given, replaces
// maintenance.message. Running instances apply it within a second; the
// change is recorded in the settings history and audit log as system:cli.
func SetMaintenance(ctx context.Context, cfg Config, on bool, message string, w io.Writer) error {
	deps, err := openCommandDeps(ctx, cfg, "cli")
	if err != nil {
		return err
	}
	defer deps.pool.Close()
	reg := settings.NewRegistry()
	declared := declareSettings(reg)
	store, err := settings.NewStore(ctx, deps.pool, reg, deps.recorder)
	if err != nil {
		return err
	}

	ctx = actor.With(ctx, actor.System("cli"))
	state := map[bool]string{true: "on", false: "off"}[on]
	reason := "maintenance mode turned " + state + " from the command line"
	set := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		current, err := store.Get(key)
		if err != nil {
			return err
		}
		if _, err := store.Set(ctx, key, raw, settings.Change{Version: current.Version, Reason: reason}); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
		return nil
	}
	if message != "" {
		if err := set(declared.maintenanceMessage.Key(), message); err != nil {
			return err
		}
	}
	if err := set(declared.maintenanceEnabled.Key(), on); err != nil {
		return err
	}
	if on {
		fmt.Fprintf(w, "maintenance mode is on: every instance answers 503 within a second, except health checks, docs, sign-in and /ops\n")
	} else {
		fmt.Fprintf(w, "maintenance mode is off\n")
	}
	return nil
}
