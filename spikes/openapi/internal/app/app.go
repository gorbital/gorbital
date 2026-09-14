// Package app is the composition root of the spike: it builds the API,
// installs error handling, access control and docs, and wires modules.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"apistock.dev/spikes/openapi/internal/modules/projects"
	projectrepository "apistock.dev/spikes/openapi/internal/modules/projects/repository"
)

// New returns the HTTP handler and the Huma API (for spec export).
func New(logger *slog.Logger) (http.Handler, huma.API) {
	installErrorHandling(logger)

	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("my-api", "0.1.0")
	cfg.DocsPath = "" // served below so assets can be embedded later
	cfg.CreateHooks = nil
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer", Description: "Session token"},
	}
	api := humago.New(mux, cfg)

	access := &accessControl{api: api, members: demoMembers}
	projects.New(projectrepository.NewMemory(), newID("prj_"), time.Now).Register(api, access.require)

	mux.HandleFunc("GET /docs", docs)
	return requestID(mux), api
}

// demoMembers maps a session token (user) to organisation permissions.
var demoMembers = map[string]map[string][]string{
	"alice": {"org_acme": {"projects:create", "projects:read", "projects:update"}},
	"bob":   {"org_globex": {"projects:create", "projects:read", "projects:update"}, "org_acme": {"projects:read"}},
}

type accessControl struct {
	api     huma.API
	members map[string]map[string][]string
}

func (a *accessControl) require(permission string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		user, ok := strings.CutPrefix(ctx.Header("Authorization"), "Bearer ")
		if !ok || user == "" {
			_ = huma.WriteErr(a.api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		perms, member := a.members[user][ctx.Param("orgId")]
		if !member || !slices.Contains(perms, permission) {
			_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, "missing permission "+permission)
			return
		}
		next(ctx)
	}}
}

type requestIDKey struct{}

func requestID(next http.Handler) http.Handler {
	gen := newID("req_")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = gen()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFrom returns the request ID stored by the requestID middleware.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newID(prefix string) func() string {
	return func() string {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		return prefix + hex.EncodeToString(b)
	}
}

// docs serves Scalar. The spike loads it from a CDN; the real template embeds
// the pinned asset so /docs works offline.
func docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><title>my-api docs</title><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body>
<script id="api-reference" data-url="/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.44.20/dist/browser/standalone.js"></script>
</body>
</html>`))
}
