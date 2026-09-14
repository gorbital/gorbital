package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/buildinfo"
	"apistock.dev/httpx"
	"apistock.dev/modules/openapi"
	"apistock.dev/page"
	"apistock.dev/ratelimit"
)

// authRequestsPerMinute bounds sign-up, sign-in, code and password requests
// per client IP address, on top of the auth module's per-account limits.
const authRequestsPerMinute = 60

// authLimitKey limits changing requests to /v1/auth/ by client IP. Behind a
// proxy, add trusted-proxy middleware so RemoteAddr is the client.
func authLimitKey(r *http.Request) string {
	if r.Method == http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/auth/") {
		return ""
	}
	return ratelimit.ByRemoteIP(r)
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
	// Pagination errors, shared by every list endpoint that uses apistock.dev/page.
	err = mapper.Add(
		httpx.Mapping{Err: page.ErrInvalidCursor, Status: http.StatusBadRequest, Code: "invalid_cursor", Detail: "the cursor is not valid"},
		httpx.Mapping{Err: page.ErrInvalidSort, Status: http.StatusBadRequest, Code: "invalid_sort", Detail: "sort by one allowed field, with - for descending order"},
		httpx.Mapping{Err: page.ErrInvalidLimit, Status: http.StatusBadRequest, Code: "invalid_limit", Detail: "limit must be between 1 and 100"},
	)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	api := openapi.New(mux, ServiceName, buildinfo.Read().Version,
		openapi.WithBearerAuth(`Session token from POST /v1/auth/login with "transport": "bearer". Browsers use the session cookie that login sets instead.`),
	)

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

	mux.Handle("GET /livez", a.health.Liveness())
	mux.Handle("GET /readyz", a.health.Readiness())
	if a.cfg.DocsEnabled {
		openapi.MountDocs(mux, openapi.DocsOptions{Title: ServiceName + " API"})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_found", "no route matches "+r.Method+" "+r.URL.Path))
	})

	cors, err := httpx.CORS(httpx.CORSOptions{AllowedOrigins: a.cfg.CORSOrigins})
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
		httpx.RequestID(),
		a.tel.HTTPMiddleware(),
		httpx.AccessLog(a.logger),
		httpx.SecureHeaders(httpx.SecureHeadersOptions{HSTSMaxAge: hsts}),
		cors,
		crossOrigin, // protects cookie-authenticated requests from other sites
		httpx.BodyLimit(a.cfg.MaxBodyBytes),
	}
	if svc.auth != nil {
		middlewares = append(middlewares,
			svc.auth.Middleware(a.logger),
			ratelimit.Middleware(ratelimit.New(authRequestsPerMinute/60.0, authRequestsPerMinute), authLimitKey, nil),
		)
	}
	a.api = api
	a.handler = httpx.Chain(mux, middlewares...)
	return nil
}
