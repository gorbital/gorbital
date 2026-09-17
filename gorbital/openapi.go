package gorbital

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/buildinfo"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/idempotency"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/openapi/reference"
	"gorbital.dev/page"
)

// bearerDescription describes the bearer security scheme in the OpenAPI
// document, as a v0.1 app's routes.go does.
const bearerDescription = `Session token from POST /v1/auth/login with "transport": "bearer", or an API key (gbk_…) from POST /v1/auth/api-keys. Browsers use the session cookie that login sets instead.`

type versionOutput struct {
	Body buildinfo.Info
}

// buildAPI creates the API with every module's routes on a new mux: the
// OpenAPI document, /version, the docs when enabled, and problem+json for
// unknown routes. Health checks and the middleware are the caller's.
func buildAPI(cfg Config, o options, modules []Module, deps Deps, orgs OrgAuthorizer) (huma.API, *http.ServeMux, *registry, error) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	mapper, err := httpx.NewMapper(logger)
	if err != nil {
		return nil, nil, nil, err
	}
	openapi.InstallErrors(mapper)
	// Pagination errors, shared by every list endpoint that uses gorbital.dev/page.
	if err := mapper.Add(
		httpx.Mapping{Err: page.ErrInvalidCursor, Status: http.StatusBadRequest, Code: "invalid_cursor", Detail: "the cursor is not valid"},
		httpx.Mapping{Err: page.ErrInvalidSort, Status: http.StatusBadRequest, Code: "invalid_sort", Detail: "sort by one allowed field, with - for descending order"},
		httpx.Mapping{Err: page.ErrInvalidLimit, Status: http.StatusBadRequest, Code: "invalid_limit", Detail: "limit must be between 1 and 100"},
	); err != nil {
		return nil, nil, nil, err
	}

	mux := http.NewServeMux()
	apiOpts := []openapi.Option{openapi.WithBearerAuth(bearerDescription)}
	if !cfg.DocsEnabled {
		apiOpts = append(apiOpts, openapi.WithoutSpecEndpoints()) // APP_DOCS_ENABLED=false hides the contract too
	}
	api := openapi.New(mux, o.name, buildinfo.Read().Version, apiOpts...)
	huma.Register(api, huma.Operation{
		OperationID: "get-version",
		Method:      http.MethodGet,
		Path:        "/version",
		Summary:     "Build information",
		Tags:        []string{"System"},
	}, func(context.Context, *struct{}) (*versionOutput, error) {
		return &versionOutput{Body: buildinfo.Read()}, nil
	})

	reg, err := mount(api, mapper, deps, orgs, modules)
	if err != nil {
		return nil, nil, nil, err
	}
	documentIdempotencyKey(api)

	if cfg.DocsEnabled {
		openapi.MountDocs(mux, openapi.DocsOptions{Title: o.name + " API"})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_found", "no route matches "+r.Method+" "+r.URL.Path))
	})
	return api, mux, reg, nil
}

// documentIdempotencyKey adds the optional Idempotency-Key header to every
// POST and PATCH operation the Idempotency step applies to.
func documentIdempotencyKey(api huma.API) {
	minLength, maxLength := 1, idempotency.MaxKeyLength
	for path, item := range api.OpenAPI().Paths {
		if strings.HasPrefix(path, signInPrefix) {
			continue
		}
		for _, op := range []*huma.Operation{item.Post, item.Patch} {
			if op == nil {
				continue
			}
			op.Parameters = append(op.Parameters, &huma.Param{
				Name: idempotency.Header,
				In:   "header",
				Description: "A unique value, such as a UUID, that makes retrying this request safe: a retry with the same key and " +
					"request gets the first response again, with `Idempotent-Replayed: true`. The same key with another request " +
					"fails with 422 `idempotency_key_reused`, and while the first request runs with 409 `idempotency_in_progress`. " +
					"Only for signed-in callers; keys and responses are kept for 24 hours by default.",
				Example: "7f1e9c4a-4f5b-4c1e-9a55-2d1d7c2b8e61",
				Schema:  &huma.Schema{Type: "string", MinLength: &minLength, MaxLength: &maxLength, Pattern: "^[!-~]+$"},
			})
		}
	}
}

// writeOpenAPI writes the OpenAPI document of the app o describes, built
// without a database: modules register their routes with zero Deps.
func writeOpenAPI(w io.Writer, o options) error {
	cfg, err := LoadConfig(exportSource)
	if err != nil {
		return err
	}
	modules := o.allModules()
	if err := validateModules(modules); err != nil {
		return err
	}
	api, _, _, err := buildAPI(cfg, o, modules, Deps{}, nil)
	if err != nil {
		return err
	}
	if err := openapi.WriteSpec(w, api); err != nil {
		return fmt.Errorf("export openapi: %w", err)
	}
	return nil
}

// apiFileNames are the files writeAPIFiles writes, in api/ by convention.
var apiFileNames = []string{"openapi.json", "postman_collection.json", "llms.txt"}

// writeAPIFiles writes the OpenAPI document, a Postman collection and
// llms.txt into dir (ADR-0027, ADR-0051).
func writeAPIFiles(dir string, o options) error {
	var spec bytes.Buffer
	if err := writeOpenAPI(&spec, o); err != nil {
		return err
	}
	postman, err := reference.Postman(spec.Bytes(), reference.ExportOptions{BaseURL: "http://localhost:8080"})
	if err != nil {
		return fmt.Errorf("export Postman collection: %w", err)
	}
	llms, err := reference.LLMs(spec.Bytes(), reference.ExportOptions{})
	if err != nil {
		return fmt.Errorf("export llms.txt: %w", err)
	}
	files := map[string][]byte{"openapi.json": spec.Bytes(), "postman_collection.json": postman, "llms.txt": llms}
	for _, name := range apiFileNames {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil { //nolint:gosec // public API documents committed to the repository
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}
