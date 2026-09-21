// Package routes lists an app's HTTP routes for orb routes and the Dev
// Portal (ADR-0082, ADR-0083): what each route is, from the app's OpenAPI
// document (method, path, operation ID, summary, tags, guards from
// x-gorbital-guards, whether it is public), joined with where it is in the
// app's source, found with go/parser (the registration's file and line, the
// handler and its declaration, the module, and the middleware gorbital.Use
// and Module.Middleware add).
//
// The document is the truth about what the app serves; the source scan is
// best effort: a route registered through a helper that builds its path at
// run time has no source position. Apps on the v0.1 layout have no
// x-gorbital-guards and no gorbital.Use, so their routes list no guards or
// middleware, and List.GuardsKnown is false.
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Pos is a position in the app's source. File is slash-separated and
// relative to the app directory.
type Pos struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

func (p *Pos) String() string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", p.File, p.Line)
}

// Route is one operation of the app. Its JSON form is public API (orb routes
// --json, ADR-0054): fields may be added, never removed, renamed or retyped.
type Route struct {
	// Method is upper case, such as GET.
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	OperationID string   `json:"operation_id"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
	// Module is the module the route was found in (internal/modules/<dir>,
	// or the Name of its gorbital.Module); empty without a source position.
	Module string `json:"module"`
	// Handler is the handler expression, such as h.createBook.
	Handler string `json:"handler"`
	// Source is where the route is registered: the gorbital.Get (or
	// huma.Register) call. Nil when the scan didn't find it.
	Source *Pos `json:"source"`
	// HandlerSource is the handler's declaration, when found in the same
	// package.
	HandlerSource *Pos `json:"handler_source"`
	// Guards are x-gorbital-guards: "public" or "authenticated", then each
	// guard, such as "permission:books.book.read".
	Guards []string `json:"guards"`
	// Middleware are the expressions of Module.Middleware, then the groups'
	// gorbital.Use (outer first), then the route's, in the order they run.
	Middleware []string `json:"middleware"`
	// Public reports a route that needs no sign-in.
	Public bool `json:"public"`
	// Scope is the access rule the route serves under (ADR-0091): the
	// app's tenant ("organisation", "merchant"), "user", "public" or
	// "custom". It is the scope the module was generated with, as
	// gorbital.yaml records it, or what the route's own guards say; empty
	// when neither knows, as for a library module's routes.
	Scope      string `json:"scope"`
	Deprecated bool   `json:"deprecated"`
}

// List is every route of an app.
type List struct {
	App string `json:"app"`
	// Source is where the OpenAPI document came from: "app" (the running
	// app's /openapi.json), "export" (go run ./cmd/api openapi) or "file".
	Source string `json:"source"`
	// GuardsKnown is false when the document has no x-gorbital-guards, as
	// in apps on the v0.1 layout: routes then list no guards.
	GuardsKnown bool    `json:"guards_known"`
	Total       int     `json:"total"`
	Public      int     `json:"public"`
	Routes      []Route `json:"routes"`
	// Warnings explain what couldn't be found, such as routes without a
	// source position.
	Warnings []string `json:"warnings"`

	// unplaced is the warning about routes without a source position, which
	// Filter drops when it keeps only the app's routes.
	unplaced string
}

// Sources of the OpenAPI document.
const (
	SourceApp    = "app"
	SourceExport = "export"
	SourceFile   = "file"
)

// Build returns the routes of the app in dir from its OpenAPI document doc,
// with positions from the app's source.
func Build(app, dir string, doc []byte, source string) (List, error) {
	routes, guardsKnown, err := FromOpenAPI(doc)
	if err != nil {
		return List{}, err
	}
	list := List{App: app, Source: source, GuardsKnown: guardsKnown, Routes: routes, Warnings: []string{}}
	found, err := Scan(dir)
	if err != nil {
		list.Warnings = append(list.Warnings, "the source couldn't be scanned: "+err.Error())
	}
	missing := 0
	for i := range list.Routes {
		r := &list.Routes[i]
		if s, ok := found.match(r.Method, r.Path, r.OperationID); ok {
			r.Module, r.Handler, r.Source, r.HandlerSource = s.module, s.handler, s.pos, s.handlerPos
			r.Middleware = append(r.Middleware, s.middleware...)
		} else {
			missing++
		}
		if r.Public {
			list.Public++
		}
	}
	list.Total = len(list.Routes)
	if missing > 0 {
		list.unplaced = fmt.Sprintf("%d of %d routes have no source position: registered by a library module (such as authhttp or opshttp), or through code that builds the path at run time", missing, list.Total)
		list.Warnings = append(list.Warnings, list.unplaced)
	}
	if !guardsKnown && list.Total > 0 {
		list.Warnings = append(list.Warnings, "the OpenAPI document has no x-gorbital-guards (an app on the v0.1 layout): guards and middleware aren't listed, and public means no security requirement")
	}
	return list, nil
}

// Scopes fills in each route's access rule (ADR-0091): modules maps a
// module's directory to the scope it was generated with, as the app's
// gorbital.yaml records it, and tenant is what the app calls its tenant.
// A route whose module records a scope takes it; otherwise a public route
// is "public" and a route with a scope guard is the tenant's. Nothing is
// inferred from a plain permission guard, which says who may call a route
// and not whose records it serves.
func (l List) Scopes(modules map[string]string, tenant string) List {
	if tenant == "" {
		tenant = "tenant"
	}
	scoped := make([]Route, len(l.Routes))
	copy(scoped, l.Routes)
	for i := range scoped {
		r := &scoped[i]
		switch scope := modules[r.Module]; {
		case scope == "tenant":
			r.Scope = tenant
		case scope != "":
			r.Scope = scope
		case r.Public:
			r.Scope = "public"
		case slices.ContainsFunc(r.Guards, isScopeGuard):
			r.Scope = tenant
		}
	}
	l.Routes = scoped
	return l
}

// isScopeGuard reports a guard that asks the app's scope authorizer:
// guard.Scope, or its old name guard.OrgMember.
func isScopeGuard(guard string) bool {
	return strings.HasPrefix(guard, "scope:") || strings.HasPrefix(guard, "org_member:")
}

// Filter keeps the routes of module (when not empty), only public ones with
// publicOnly, and only the routes found in the app's source with appOnly,
// leaving out those of library modules; it recounts.
func (l List) Filter(module string, publicOnly, appOnly bool) List {
	kept := make([]Route, 0, len(l.Routes))
	l.Public = 0
	for _, r := range l.Routes {
		if (module != "" && r.Module != module) || (publicOnly && !r.Public) || (appOnly && r.Source == nil) {
			continue
		}
		kept = append(kept, r)
		if r.Public {
			l.Public++
		}
	}
	l.Routes, l.Total = kept, len(kept)
	if appOnly && l.unplaced != "" {
		l.Warnings = slices.DeleteFunc(slices.Clone(l.Warnings), func(w string) bool { return w == l.unplaced })
		l.unplaced = ""
	}
	return l
}

var methodOrder = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE"}

// FromOpenAPI returns the operations of an OpenAPI 3 document, sorted by
// path and method, and whether any carries x-gorbital-guards.
func FromOpenAPI(doc []byte) ([]Route, bool, error) {
	type operation struct {
		OperationID string                 `json:"operationId"`
		Summary     string                 `json:"summary"`
		Tags        []string               `json:"tags"`
		Security    *[]map[string][]string `json:"security"`
		Deprecated  bool                   `json:"deprecated"`
		Guards      *[]string              `json:"x-gorbital-guards"`
	}
	var spec struct {
		OpenAPI  string                                `json:"openapi"`
		Security []map[string][]string                 `json:"security"`
		Paths    map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(doc, &spec); err != nil {
		return nil, false, fmt.Errorf("the OpenAPI document isn't valid JSON: %w", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") {
		return nil, false, errors.New("not an OpenAPI 3 document")
	}
	var routes []Route
	guardsKnown := false
	for path, item := range spec.Paths {
		for method, raw := range item {
			upper := strings.ToUpper(method)
			if !slices.Contains(methodOrder, upper) {
				continue // parameters, summary and the like
			}
			var op operation
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, false, fmt.Errorf("%s %s: %w", upper, path, err)
			}
			r := Route{
				Method: upper, Path: path, OperationID: op.OperationID, Summary: op.Summary,
				Tags: op.Tags, Deprecated: op.Deprecated, Guards: []string{}, Middleware: []string{},
			}
			if r.Tags == nil {
				r.Tags = []string{}
			}
			if op.Guards != nil {
				guardsKnown = true
				r.Guards = *op.Guards
				r.Public = slices.Contains(r.Guards, "public")
			} else {
				security := spec.Security
				if op.Security != nil {
					security = *op.Security
				}
				r.Public = len(security) == 0
			}
			routes = append(routes, r)
		}
	}
	slices.SortFunc(routes, func(a, b Route) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return slices.Index(methodOrder, a.Method) - slices.Index(methodOrder, b.Method)
	})
	return routes, guardsKnown, nil
}

// ExportTimeout bounds go run ./cmd/api openapi, including a cold build.
const ExportTimeout = 3 * time.Minute

// Export builds the app in dir and returns its OpenAPI document, as
// go run ./cmd/api openapi --dir writes it. env is the command's
// environment (nil inherits orb's).
func Export(ctx context.Context, dir string, env []string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "orb-routes-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(ctx, ExportTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/api", "openapi", "--dir", tmp)
	cmd.Dir, cmd.Env = dir, env
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go run ./cmd/api openapi: %w: %s", err, firstLines(string(out), 5))
	}
	return os.ReadFile(filepath.Join(tmp, "openapi.json"))
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[:min(n, len(lines))], "\n")
}
