package recipes

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"path"
	"strconv"
	"strings"
	"text/template"
)

// The module templates of orb gen module (ADR-0083 §3, D13): a module for
// apps on gorbital.Main, layered, with one file per operation in usecase/,
// repository/ and delivery/. examples/apps/shelfie's shelves module is the
// golden output.
//
//go:embed module/*.tmpl
var moduleFS embed.FS

// ModulesDir is where an app on gorbital.Main keeps its modules.
const ModulesDir = "internal/modules"

// ArchitectureTestPath is the test that checks every module's layers.
const ArchitectureTestPath = ModulesDir + "/architecture_test.go"

// reservedModuleNames can't name a generated module's package or the
// variables its code declares: they are the layers, the packages the
// generated code imports, and its local variables.
var reservedModuleNames = func() map[string]bool {
	names := map[string]bool{}
	for _, n := range strings.Fields(`domain usecase repository delivery modules migrations gorbital gorbitaltest
		guard httpx http page actor audit postgres pgx pgxpool context errors fmt slog rand base32 time strings
		utf8 slices url testing any bool byte error int string true false nil append len cap make new min max
		ctx err in out h r s c q id tx sql rows tag next current changed fields owner none sort after items last same blank choice later kept unchanged value msg errs values f now fe e v i t b w ok name edit tests body list first status dir cmp queries sorts desc key handlers routes changes invalid problem item trail action filtered website collection titles bobs
		res req app ada bob svc known cursor limit want got tt invalid reader created updated title`) {
		names[n] = true
	}
	return names
}()

// ModuleData fills the module templates: a resource's names and fields
// (ResourceData, always owned by a user until organisation guards exist),
// with the generated code's own helpers.
type ModuleData struct {
	ResourceData
}

// NewModuleData validates a module and derives every name. fields come from
// ParseModuleFields. Organisation scope isn't available yet, so o.Scope must
// be empty or ScopeUser.
func NewModuleData(module, name string, fields []Field, o ResourceOptions) (ModuleData, error) {
	if o.Scope == ScopeOrg {
		return ModuleData{}, errors.New("organisation-scoped modules arrive with guard.OrgMember (v0.2 Phase 7); generate a user-owned module for now")
	}
	o.Scope, o.RLS = ScopeUser, false
	r, err := NewResourceData(module, name, fields, o)
	if err != nil {
		return ModuleData{}, err
	}
	d := ModuleData{ResourceData: r}
	for _, ident := range []string{d.Package, d.Var, d.PluralVar()} {
		if reservedModuleNames[ident] || token.IsKeyword(ident) {
			return ModuleData{}, fmt.Errorf("%q would clash with a name the generated code uses; choose another name or --plural", ident)
		}
	}
	return d, nil
}

// ValidateModuleName reports whether name can name a module, before its
// fields are known.
func ValidateModuleName(name string) error {
	title := []Field{{Name: "title", Ident: "Title", Human: "title", Kind: KindString}}
	_, err := NewModuleData("example.com/app", name, title, ResourceOptions{Migration: "20260101000000"})
	return err
}

// PluralVar is the plural as a Go variable, such as orderItems.
func (d ModuleData) PluralVar() string {
	return strings.ToLower(d.Plural[:1]) + d.Plural[1:]
}

// Dir is the module's directory, such as internal/modules/shelves.
func (d ModuleData) Dir() string { return ModulesDir + "/" + d.Package }

// ImportPath is the module package's import path.
func (d ModuleData) ImportPath() string { return d.Module + "/" + d.Dir() }

// MigrationPath is the module's migration.
func (d ModuleData) MigrationPath() string {
	return "db/migrations/" + d.Migration + "_" + d.Table + ".sql"
}

// RoutePath is the collection's path, such as /v1/shelves.
func (d ModuleData) RoutePath() string { return "/v1/" + d.Route }

// PermRead and PermWrite are the module's permission names.
func (d ModuleData) PermRead() string { return d.Package + "." + d.Snake + ".read" }

// PermWrite is the permission the module's writes need.
func (d ModuleData) PermWrite() string { return d.Package + "." + d.Snake + ".write" }

// SampleFields is ResourceData.SampleFields for the domain package's own
// name.
func (d ModuleData) SampleFields(title string) string { return d.sampleFields(title, "domain.") }

// BodyFields are the members of a map[string]any request body in the HTTP
// tests, with title as the title and other unique fields made from it, so
// bodies with different titles don't clash; enum fields are left out, so they take
// their defaults.
func (d ModuleData) BodyFields(title string) string {
	var members []string
	for _, f := range d.TextFields() {
		value := "Example " + f.Human
		switch {
		case f.Name == d.Title().Name:
			value = title
		case f.Unique:
			value = strings.TrimSpace(title) + " " + f.Human
		}
		members = append(members, strconv.Quote(f.Name)+": "+strconv.Quote(value))
	}
	return strings.Join(members, ", ")
}

// BodyDuplicate are body members that reuse, in capitals, the first unique
// field of the tests' "Website" and differ in every other unique field.
func (d ModuleData) BodyDuplicate() string {
	unique, title := d.FirstUnique(), d.Title()
	var members []string
	for _, f := range d.TextFields() {
		value := "Example " + f.Human
		switch {
		case f.Name == unique.Name && f.Name == title.Name:
			value = "WEBSITE"
		case f.Name == unique.Name:
			value = "WEBSITE " + strings.ToUpper(f.Human)
		case f.Name == title.Name:
			value = "Other"
		case f.Unique:
			value = "Other " + f.Human
		}
		members = append(members, strconv.Quote(f.Name)+": "+strconv.Quote(value))
	}
	return strings.Join(members, ", ")
}

// BodyChoice is a body member setting the first enum field to its last
// value, starting with a comma, or "".
func (d ModuleData) BodyChoice() string {
	if !d.HasEnums() {
		return ""
	}
	e := d.FirstEnum()
	return ", " + strconv.Quote(e.Name) + ": " + strconv.Quote(e.LastValue().Value)
}

// moduleTemplates maps each template to the file it renders, relative to
// the app.
func (d ModuleData) moduleTemplates() []struct{ tmpl, path string } {
	dir := d.Dir() + "/"
	return []struct{ tmpl, path string }{
		{"module.go", dir + "module.go"},
		{"module_test.go", dir + d.Package + "_test.go"},
		{"domain.go", dir + "domain/" + d.Snake + ".go"},
		{"domain_errors.go", dir + "domain/errors.go"},
		{"domain_test.go", dir + "domain/" + d.Snake + "_test.go"},
		{"usecase_service.go", dir + "usecase/service.go"},
		{"usecase_ports.go", dir + "usecase/ports.go"},
		{"usecase_create.go", dir + "usecase/create_" + d.Snake + ".go"},
		{"usecase_get.go", dir + "usecase/get_" + d.Snake + ".go"},
		{"usecase_list.go", dir + "usecase/list_" + d.Table + ".go"},
		{"usecase_update.go", dir + "usecase/update_" + d.Snake + ".go"},
		{"usecase_delete.go", dir + "usecase/delete_" + d.Snake + ".go"},
		{"repository_store.go", dir + "repository/store.go"},
		{"repository_insert.go", dir + "repository/insert_" + d.Snake + ".go"},
		{"repository_select.go", dir + "repository/select_" + d.Snake + ".go"},
		{"repository_select_list.go", dir + "repository/select_" + d.Table + ".go"},
		{"repository_update.go", dir + "repository/update_" + d.Snake + ".go"},
		{"repository_delete.go", dir + "repository/delete_" + d.Snake + ".go"},
		{"delivery_routes.go", dir + "delivery/routes.go"},
		{"delivery_responses.go", dir + "delivery/responses.go"},
		{"delivery_create.go", dir + "delivery/create_" + d.Snake + ".go"},
		{"delivery_get.go", dir + "delivery/get_" + d.Snake + ".go"},
		{"delivery_list.go", dir + "delivery/list_" + d.Table + ".go"},
		{"delivery_update.go", dir + "delivery/update_" + d.Snake + ".go"},
		{"delivery_delete.go", dir + "delivery/delete_" + d.Snake + ".go"},
		{"migration.sql", d.MigrationPath()},
	}
}

// RenderModule renders a module's files and its migration, in the order
// orb gen module lists them. Go output is formatted with gofmt.
func RenderModule(d ModuleData) ([]JobFile, error) {
	targets := d.moduleTemplates()
	files := make([]JobFile, 0, len(targets))
	for _, target := range targets {
		f, err := renderModuleTemplate(target.tmpl, target.path, d)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// RenderArchitectureTest renders internal/modules/architecture_test.go for
// the app whose module path is module: the layer rules of every module
// (ADR-0039, ADR-0083). orb gen module writes it when the app has none.
func RenderArchitectureTest(module string) (JobFile, error) {
	return renderModuleTemplate("architecture_test.go", ArchitectureTestPath, struct{ Module string }{module})
}

func renderModuleTemplate(tmplName, target string, data any) (JobFile, error) {
	name := "module/" + tmplName + ".tmpl"
	src, err := moduleFS.ReadFile(name)
	if err != nil {
		return JobFile{}, err
	}
	funcs := template.FuncMap{"quote": strconv.Quote, "upper": strings.ToUpper, "lower": strings.ToLower}
	tmpl, err := template.New(name).Delims("⟦", "⟧").Funcs(funcs).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return JobFile{}, fmt.Errorf("recipes: parse %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return JobFile{}, fmt.Errorf("recipes: render %s: %w", name, err)
	}
	content := out.Bytes()
	if path.Ext(target) == ".go" {
		formatted, err := format.Source(content)
		if err != nil {
			return JobFile{}, fmt.Errorf("recipes: %s is not valid Go after rendering: %w\n%s", target, err, content)
		}
		content = formatted
	}
	return JobFile{Path: target, Content: content}, nil
}
