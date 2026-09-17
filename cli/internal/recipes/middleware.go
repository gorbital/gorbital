package recipes

import (
	"errors"
	"fmt"
	"go/token"
	"regexp"
	"strings"
)

// Kinds of orb gen middleware.
const (
	// MiddlewareModule is middleware in a module's delivery package, added
	// with gorbital.Use or Module.Middleware.
	MiddlewareModule = "module"
	// MiddlewareGlobal is middleware in internal/middleware, added to every
	// route with gorbital.WithMiddleware.
	MiddlewareGlobal = "global"
	// MiddlewareGuard is a guard.New guard in a module's delivery package,
	// with the error it refuses requests with.
	MiddlewareGuard = "guard"
)

// MiddlewareDir is the package orb gen middleware --global writes into.
const MiddlewareDir = "internal/middleware"

var moduleDirPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// reservedMiddlewareNames are declared by the generated modules' delivery
// packages or the generated files themselves.
var reservedMiddlewareNames = map[string]bool{
	"Register": true, "Module": true, "Middleware": true, "Handler": true, "New": true,
}

// MiddlewareData fills the middleware and guard templates.
type MiddlewareData struct {
	// AppModule is the app's module path.
	AppModule string
	// Kind is MiddlewareModule, MiddlewareGlobal or MiddlewareGuard.
	Kind string
	// ModuleName is the module whose delivery package holds it; empty for
	// global middleware.
	ModuleName string
	Ident      string // RequireClientVersion
	Snake      string // require_client_version: file, guard and code names
	Human      string // require client version
}

// NewMiddlewareData validates orb gen middleware's name and kind.
func NewMiddlewareData(appModule, name, kind, moduleName string) (MiddlewareData, error) {
	name = strings.TrimSpace(name)
	if len(name) > 40 || !resourceNamePattern.MatchString(name) {
		return MiddlewareData{}, errors.New("middleware name must start with a letter and use letters, digits, hyphens or underscores (max 40), such as RequireClientVersion")
	}
	words := splitWords(name)
	d := MiddlewareData{
		AppModule: appModule, Kind: kind, ModuleName: moduleName,
		Ident: identFromWords(words), Snake: strings.Join(words, "_"), Human: strings.Join(words, " "),
	}
	switch kind {
	case MiddlewareGlobal:
		if moduleName != "" {
			return MiddlewareData{}, errors.New("global middleware belongs to no module")
		}
	case MiddlewareModule, MiddlewareGuard:
		if !moduleDirPattern.MatchString(moduleName) || token.IsKeyword(moduleName) {
			return MiddlewareData{}, fmt.Errorf("module %q must be the name of a directory in internal/modules, such as books", moduleName)
		}
	default:
		return MiddlewareData{}, fmt.Errorf("unknown middleware kind %q", kind)
	}
	if !token.IsIdentifier(d.Ident) || reservedMiddlewareNames[d.Ident] || strings.HasSuffix(d.Snake, "_test") {
		return MiddlewareData{}, fmt.Errorf("%q can't name middleware; choose another name", name)
	}
	return d, nil
}

// Package is the Go package the files go in.
func (d MiddlewareData) Package() string {
	if d.Kind == MiddlewareGlobal {
		return "middleware"
	}
	return "delivery"
}

// Dir is the package's directory, relative to the app.
func (d MiddlewareData) Dir() string {
	if d.Kind == MiddlewareGlobal {
		return MiddlewareDir
	}
	return ModulesDir + "/" + d.ModuleName + "/delivery"
}

// File is the middleware's file; Test is its test.
func (d MiddlewareData) File() string { return d.Dir() + "/" + d.Snake + ".go" }

// Test is the middleware's test file.
func (d MiddlewareData) Test() string { return d.Dir() + "/" + d.Snake + "_test.go" }

// Check is the unexported function holding the rule.
func (d MiddlewareData) Check() string { return "check" + d.Ident }

// Err is a guard's refusal error.
func (d MiddlewareData) Err() string { return "Err" + d.Ident + "Refused" }

// Code is a guard's problem code.
func (d MiddlewareData) Code() string { return d.Snake + "_refused" }

// Declared are the package-level names the files declare, which must be
// free in the package.
func (d MiddlewareData) Declared() []string {
	names := []string{d.Ident, d.Check(), "Test" + d.Ident}
	if d.Kind == MiddlewareGuard {
		names = append(names, d.Err())
	}
	return names
}

// Wire is the Go code that puts the middleware to use, for the developer to
// add: orb gen middleware never edits routes.go, module.go or main.go.
func (d MiddlewareData) Wire() string {
	switch d.Kind {
	case MiddlewareGlobal:
		return "gorbital.WithMiddleware(middleware." + d.Ident + ")"
	case MiddlewareGuard:
		return d.Ident + "()"
	}
	return "gorbital.Use(" + d.Ident + ")"
}

// ErrorMapping is the line mapping a guard's error, for module.go's Errors.
func (d MiddlewareData) ErrorMapping() string {
	return fmt.Sprintf("{Err: delivery.%s, Status: http.StatusForbidden, Code: %q, Detail: %q},", d.Err(), d.Code(), "this request isn't allowed")
}

// RenderMiddleware renders the middleware or guard and its table-driven
// test.
func RenderMiddleware(d MiddlewareData) ([]JobFile, error) {
	tmpl := "middleware"
	if d.Kind == MiddlewareGuard {
		tmpl = "guard"
	}
	var files []JobFile
	for _, t := range []struct{ tmpl, path string }{{tmpl + ".go", d.File()}, {tmpl + "_test.go", d.Test()}} {
		f, err := renderModuleTemplate(t.tmpl, t.path, d)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// MiddlewareDocPath is the package comment of internal/middleware.
const MiddlewareDocPath = MiddlewareDir + "/doc.go"

// RenderMiddlewareDoc renders internal/middleware/doc.go, which orb gen
// middleware --global writes with the package's first middleware.
func RenderMiddlewareDoc() (JobFile, error) {
	return renderModuleTemplate("middleware_doc.go", MiddlewareDocPath, nil)
}
