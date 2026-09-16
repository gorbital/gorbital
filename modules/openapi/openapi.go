// Package openapi integrates Huma with gorbital (ADR-0027): an API on the
// standard http.ServeMux, problem+json errors produced by the application's
// error mapper, an API reference at /docs in the gorbital design (ADR-0049,
// rendered by package reference), and OpenAPI export.
//
// Huma is used only in delivery layers and the composition root of
// generated apps; domain, use case and repository code never imports it.
//
// Stability: stable (ADR-0015, ADR-0054).
package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Bearer is the security requirement for operations that need a session
// token. Use it as huma.Operation.Security.
var Bearer = []map[string][]string{{BearerScheme: {}}}

// BearerScheme is the name of the bearer security scheme.
const BearerScheme = "bearer"

// An Option configures [New].
type Option interface{ apply(*huma.Config) }

type optionFunc func(*huma.Config)

func (f optionFunc) apply(c *huma.Config) { f(c) }

// WithDescription sets the API description shown in the docs.
func WithDescription(markdown string) Option {
	return optionFunc(func(c *huma.Config) { c.Info.Description = markdown })
}

// WithBearerAuth declares the bearer security scheme used by [Bearer].
func WithBearerAuth(description string) Option {
	return optionFunc(func(c *huma.Config) {
		if c.Components.SecuritySchemes == nil {
			c.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
		}
		c.Components.SecuritySchemes[BearerScheme] = &huma.SecurityScheme{
			Type:        "http",
			Scheme:      "bearer",
			Description: description,
		}
	})
}

// New returns a Huma API registered on mux. It serves the OpenAPI document at
// /openapi.json and /openapi.yaml, disables Huma's built-in docs (use
// [MountDocs]) and response $schema links, and doesn't expose /schemas.
//
// Call [InstallErrors] before registering operations.
func New(mux *http.ServeMux, title, version string, opts ...Option) huma.API {
	cfg := huma.DefaultConfig(title, version)
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.OpenAPIPath = "/openapi"
	cfg.CreateHooks = nil
	for _, o := range opts {
		o.apply(&cfg)
	}
	return humago.New(mux, cfg)
}

// WriteSpec writes api's OpenAPI document to w as indented JSON. Generated
// apps call it from "my-api openapi" to export api/openapi.json.
func WriteSpec(w io.Writer, api huma.API) error {
	b, err := json.MarshalIndent(api.OpenAPI(), "", "  ")
	if err != nil {
		return fmt.Errorf("openapi: encode spec: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("openapi: write spec: %w", err)
	}
	return nil
}
