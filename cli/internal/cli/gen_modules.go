package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gorbital.dev/cli/internal/genplan"
)

const genModulesUsage = `Usage: orb gen modules [flags]

Writes internal/modules/modules.gen.go: the function All, which lists every
module of the app, for main.go's gorbital.WithModules(modules.All()...). A
module is a directory under internal/modules whose package declares

	func Module() gorbital.Module

The file is generated: never edit it. orb dev rewrites it when a module is
added or removed, and go generate ./internal/modules runs this command. The
app builds without orb, because the file is committed.
`

// modulesGenPath is the generated module list, relative to the app.
const modulesGenPath = "internal/modules/modules.gen.go"

// gorbitalImportPath is the composition package modules return a Module of.
const gorbitalImportPath = "gorbital.dev/gorbital"

type genModulesResult struct {
	File    string   `json:"file"`
	Modules []string `json:"modules"`
	Changed bool     `json:"changed"`
	DryRun  bool     `json:"dry_run"`
}

func runGenModules(_ context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb gen modules", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "show what would change without writing")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	flags.Bool("no-input", false, "never prompt (orb gen modules never does)")
	flags.Usage = func() {
		fmt.Fprint(stderr, genModulesUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError("orb gen modules takes no arguments")
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	plan, err := planModules(app)
	if err != nil {
		return err
	}
	result := plan.Result.(genModulesResult)
	result.DryRun = *dryRun
	if !*dryRun && result.Changed {
		if err := genplan.Apply(app.dir, plan); err != nil {
			return err
		}
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	switch {
	case !result.Changed:
		fmt.Fprintf(stdout, "✓ %s is up to date (%s)\n", modulesGenPath, moduleCount(result.Modules))
	case *dryRun:
		fmt.Fprintf(stdout, "Would write (dry run) %s: %s\n", modulesGenPath, moduleCount(result.Modules))
	default:
		fmt.Fprintf(stdout, "✓ Wrote %s: %s\n", modulesGenPath, moduleCount(result.Modules))
	}
	return nil
}

func moduleCount(modules []string) string {
	switch len(modules) {
	case 0:
		return "no modules"
	case 1:
		return "1 module: " + modules[0]
	}
	return strconv.Itoa(len(modules)) + " modules: " + strings.Join(modules, ", ")
}

// planModules returns the change orb gen modules makes: modules.gen.go
// created or rewritten, or no change when it already lists every module.
func planModules(app appInfo) (genplan.Plan, error) {
	modules, err := findModules(app)
	if err != nil {
		return genplan.Plan{}, err
	}
	content, err := renderModules(modules)
	if err != nil {
		return genplan.Plan{}, err
	}
	names := make([]string, 0, len(modules))
	for _, m := range modules {
		names = append(names, m.dir)
	}
	plan := genplan.Plan{Generator: "modules", Name: "modules", Next: []string{"go build ./..."}}
	result := genModulesResult{File: modulesGenPath, Modules: names}
	before, err := os.ReadFile(filepath.Join(app.dir, filepath.FromSlash(modulesGenPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		plan.Changes = []genplan.Change{{Path: modulesGenPath, Kind: genplan.Create, Content: content}}
	case err != nil:
		return genplan.Plan{}, err
	case !bytes.Equal(before, content):
		plan.Changes = []genplan.Change{{Path: modulesGenPath, Kind: genplan.Modify, Before: before, Content: content}}
	}
	result.Changed = len(plan.Changes) > 0
	plan.Summary = "  Module list: " + modulesGenPath + " (" + moduleCount(names) + ")\n"
	plan.Result = result
	return plan, nil
}

// appModule is a directory of internal/modules declaring func Module()
// gorbital.Module.
type appModule struct {
	dir        string // the directory's name, such as books
	pkg        string // the package's name
	importPath string
}

// findModules parses each directory directly under internal/modules and
// returns those whose package declares func Module() gorbital.Module,
// sorted by directory name. Test files and directories starting with . or
// _, and testdata, are skipped.
func findModules(app appInfo) ([]appModule, error) {
	root := filepath.Join(app.dir, "internal", "modules")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s has no internal/modules directory: create a module in internal/modules/<name> first", app.dir)
	}
	if err != nil {
		return nil, err
	}
	var modules []appModule
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
			continue
		}
		pkg, ok, err := declaresModule(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		if ok {
			modules = append(modules, appModule{dir: name, pkg: pkg, importPath: app.module + "/internal/modules/" + name})
		}
	}
	slices.SortFunc(modules, func(a, b appModule) int { return strings.Compare(a.dir, b.dir) })
	return modules, nil
}

// declaresModule reports whether the Go package in dir declares func
// Module() gorbital.Module, and the package's name.
func declaresModule(dir string) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false, err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return "", false, fmt.Errorf("parse %s: %w", filepath.Join(dir, name), err)
		}
		if moduleFunc(file) {
			return file.Name.Name, true, nil
		}
	}
	return "", false, nil
}

// moduleFunc reports whether file declares func Module() X.Module, where X
// is the file's name for gorbital.dev/gorbital.
func moduleFunc(file *ast.File) bool {
	local := ""
	for _, imp := range file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == gorbitalImportPath {
			local = "gorbital"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
	}
	if local == "" || local == "_" || local == "." {
		return false
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "Module" || fn.Type.TypeParams != nil || len(fn.Type.Params.List) > 0 {
			continue
		}
		results := fn.Type.Results
		if results == nil || len(results.List) != 1 || len(results.List[0].Names) > 1 {
			continue
		}
		sel, ok := results.List[0].Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Module" {
			continue
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
			return true
		}
	}
	return false
}

// renderModules returns modules.gen.go for modules, gofmt-formatted.
func renderModules(modules []appModule) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Code generated by orb gen modules. DO NOT EDIT.\n\n")
	b.WriteString("// Package modules lists the app's modules for main.go. Each directory\n")
	b.WriteString("// below declares one, with func Module() gorbital.Module; orb dev and\n")
	b.WriteString("// go generate rewrite this file when one is added or removed.\n")
	b.WriteString("package modules\n\n")
	b.WriteString("//go:generate orb gen modules\n\n")
	b.WriteString("import (\n\t\"gorbital.dev/gorbital\"\n")
	used := map[string]bool{"gorbital": true, "modules": true}
	refs := make([]string, len(modules))
	if len(modules) > 0 {
		b.WriteString("\n")
	}
	for i, m := range modules {
		ref := m.pkg
		if used[ref] || !token.IsIdentifier(ref) {
			ref = uniqueName(identifier(m.dir)+"module", used)
			fmt.Fprintf(&b, "\t%s %q\n", ref, m.importPath)
		} else {
			fmt.Fprintf(&b, "\t%q\n", m.importPath)
		}
		used[ref] = true
		refs[i] = ref
	}
	b.WriteString(")\n\n")
	b.WriteString("// All returns every module of the app, sorted by directory name.\n")
	b.WriteString("func All() []gorbital.Module {\n\treturn []gorbital.Module{\n")
	for _, ref := range refs {
		fmt.Fprintf(&b, "\t\t%s.Module(),\n", ref)
	}
	b.WriteString("\t}\n}\n")
	return format.Source(b.Bytes())
}

// identifier turns a directory name into a lowercase Go identifier.
func identifier(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9' && b.Len() > 0) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "m"
	}
	return b.String()
}

func uniqueName(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	return name
}

// isGorbitalApp reports whether the app in dir imports the composition
// library, whose Main serves the migrate commands.
func isGorbitalApp(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && bytes.Contains(data, []byte(gorbitalImportPath+" "))
}
