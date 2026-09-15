package app

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/buildinfo"
	"apistock.dev/httpx"
	"apistock.dev/modules/openapi"
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
	api := openapi.New(mux, ServiceName, buildinfo.Read().Version)

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
	a.handler = httpx.Chain(mux,
		httpx.Recover(a.logger),
		httpx.TrustedProxies(a.cfg.TrustedProxies), // the client's address behind load balancers (APP_TRUSTED_PROXIES)
		httpx.RequestID(),
		a.tel.HTTPMiddleware(),
		httpx.AccessLog(a.logger),
		httpx.SecureHeaders(httpx.SecureHeadersOptions{HSTSMaxAge: hsts}),
		cors,
		crossOrigin,
		httpx.BodyLimit(a.cfg.MaxBodyBytes),
	)
	return nil
}
