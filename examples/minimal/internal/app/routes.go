package app

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/buildinfo"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/telemetry"
)

type versionOutput struct {
	Body buildinfo.Info
}

// buildHTTP creates the API, registers routes and wraps them in middleware.
func (a *App) buildHTTP() error {
	mapper, err := httpx.NewMapper(a.logger)
	if err != nil {
		return err
	}
	openapi.InstallErrors(mapper)

	mux := http.NewServeMux()
	var apiOpts []openapi.Option
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

	if err := registerModules(api, mapper); err != nil {
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

	a.api = api
	// RecordRoute gives spans and metrics the matched route pattern.
	a.handler = httpx.Chain(telemetry.RecordRoute(mux),
		httpx.Recover(a.logger),
		httpx.TrustedProxies(a.cfg.TrustedProxies), // the client's address behind load balancers (APP_TRUSTED_PROXIES)
		httpx.RequestIDFrom(a.cfg.TrustedCallers),  // clients' own X-Request-ID only from APP_TRUSTED_CALLERS
		a.tel.HTTPMiddleware(),
		httpx.AccessLog(a.logger),
		httpx.SecureHeaders(httpx.SecureHeadersOptions{HSTSMaxAge: hsts}),
		cors,
		crossOrigin,
		httpx.BodyLimit(a.cfg.MaxBodyBytes),
	)
	return nil
}
