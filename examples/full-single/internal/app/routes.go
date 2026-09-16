package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/buildinfo"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/idempotency"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/telemetry"
	"gorbital.dev/page"
	"gorbital.dev/ratelimit"
	"gorbital.dev/requestid"
)

// authLimitKey limits changing requests to /v1/auth/, and Google, Apple and
// GitHub sign-in redirects, by client IP. Behind load balancers, set
// APP_TRUSTED_PROXIES so RemoteAddr is the client (ADR-0052).
func authLimitKey(r *http.Request) string {
	if !strings.HasPrefix(r.URL.Path, "/v1/auth/") {
		return ""
	}
	redirect := strings.HasSuffix(r.URL.Path, "/start") || strings.HasSuffix(r.URL.Path, "/callback")
	if r.Method == http.MethodGet && !redirect {
		return ""
	}
	return ratelimit.ByRemoteIP(r)
}

// crossSitePosts are the endpoints other sites post to by design: Apple's
// sign-in result (protected by the single-use state and the __Host-oauth
// cookie) and Apple's signed notifications (ADR-0046).
var crossSitePosts = []string{"/v1/auth/apple/callback", "/v1/auth/apple/notifications"}

// exceptCrossSitePosts applies protect to every request but crossSitePosts.
func exceptCrossSitePosts(protect httpx.Middleware) httpx.Middleware {
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

type versionOutput struct {
	Body buildinfo.Info
}

// buildHTTP creates the API, registers routes and wraps them in middleware.
func (a *App) buildHTTP(svc services) error {
	mapper, err := httpx.NewMapper(a.logger)
	if err != nil {
		return err
	}
	openapi.InstallErrors(mapper)
	// Pagination errors, shared by every list endpoint that uses gorbital.dev/page.
	err = mapper.Add(
		httpx.Mapping{Err: page.ErrInvalidCursor, Status: http.StatusBadRequest, Code: "invalid_cursor", Detail: "the cursor is not valid"},
		httpx.Mapping{Err: page.ErrInvalidSort, Status: http.StatusBadRequest, Code: "invalid_sort", Detail: "sort by one allowed field, with - for descending order"},
		httpx.Mapping{Err: page.ErrInvalidLimit, Status: http.StatusBadRequest, Code: "invalid_limit", Detail: "limit must be between 1 and 100"},
	)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	apiOpts := []openapi.Option{
		openapi.WithBearerAuth(`Session token from POST /v1/auth/login with "transport": "bearer", or an API key (gbk_…) from POST /v1/auth/api-keys. Browsers use the session cookie that login sets instead.`),
	}
	if !a.cfg.DocsEnabled {
		apiOpts = append(apiOpts, openapi.WithoutSpecEndpoints()) // APP_DOCS_ENABLED=false hides the contract too
	}
	api := openapi.New(mux, ServiceName, buildinfo.Read().Version, apiOpts...)

	huma.Register(api, huma.Operation{
		OperationID: "get-version",
		Method:      http.MethodGet,
		Path:        "/version",
		Summary:     "Build information",
		Tags:        []string{"System"},
	}, func(context.Context, *struct{}) (*versionOutput, error) {
		return &versionOutput{Body: buildinfo.Read()}, nil
	})

	if err := registerModules(api, mapper, svc); err != nil {
		return err
	}
	documentIdempotencyKey(api)

	mux.Handle("GET /livez", a.health.Liveness())
	mux.Handle("GET /readyz", a.health.Readiness())
	if a.cfg.DocsEnabled {
		openapi.MountDocs(mux, openapi.DocsOptions{Title: ServiceName + " API"})
	}
	a.mountWellKnown(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_found", "no route matches "+r.Method+" "+r.URL.Path))
	})

	cors, err := httpx.CORS(httpx.CORSOptions{
		AllowedOrigins: a.cfg.CORSOrigins,
		AllowedHeaders: []string{"Authorization", "Content-Type", requestid.Header, idempotency.Header},
		ExposedHeaders: []string{requestid.Header, "Retry-After", idempotency.ReplayedHeader},
	})
	if err != nil {
		return err
	}
	crossOrigin, err := httpx.CrossOrigin(a.cfg.CORSOrigins...)
	if err != nil {
		return err
	}
	var hsts time.Duration
	if a.cfg.Production() {
		hsts = 365 * 24 * time.Hour
	}

	middlewares := []httpx.Middleware{
		httpx.Recover(a.logger),
		httpx.TrustedProxies(a.cfg.TrustedProxies), // the client's address behind load balancers (APP_TRUSTED_PROXIES)
		httpx.RequestIDFrom(a.cfg.TrustedCallers),  // clients' own X-Request-ID only from APP_TRUSTED_CALLERS
		a.tel.HTTPMiddleware(),
		httpx.AccessLog(a.logger),
		httpx.SecureHeaders(httpx.SecureHeadersOptions{HSTSMaxAge: hsts}),
		cors,
		exceptCrossSitePosts(crossOrigin), // protects cookie-authenticated requests from other sites
		httpx.BodyLimit(a.cfg.MaxBodyBytes),
		a.maintenance(), // 503 outside health checks, docs, sign-in and /ops while maintenance.enabled is on
	}
	if svc.auth != nil {
		middlewares = append(middlewares,
			svc.auth.Middleware(a.logger),
			// auth.ip_requests_per_minute, shared by every instance (ADR-0052).
			ratelimit.Middleware(svc.ipLimiter, authLimitKey, nil),
			// Retried POST and PATCH requests with an Idempotency-Key (ADR-0060).
			idempotencyMiddleware(svc.idempotency),
		)
	}
	a.api = api
	// RecordRoute gives spans and metrics the matched route pattern, which
	// the auth middleware's request copy would otherwise hide from them.
	a.handler = httpx.Chain(telemetry.RecordRoute(mux), middlewares...)
	return nil
}
