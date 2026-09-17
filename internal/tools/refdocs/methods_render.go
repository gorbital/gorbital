package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"slices"
	"strings"
)

// methodsSite renders the Methods pages of every library package.
type methodsSite struct {
	pkgs     []*libPackage
	since    sinceIndex
	overlays map[string][]byte // by slug
}

const methodsNote = "<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->\n\n"

// pages returns the index and one page per package.
func (s *methodsSite) pages() []page {
	out := []page{{"index.md", s.index()}}
	for _, p := range s.pkgs {
		out = append(out, page{p.slug + ".md", s.renderPackage(p)})
	}
	return out
}

func (s *methodsSite) index() []byte {
	var b bytes.Buffer
	b.WriteString("# Methods\n\n")
	b.WriteString(methodsNote)
	b.WriteString("Every exported constant, variable, function, type and method of the gorbital library, one page per package: its signature, its doc comment, runnable examples from the package's `Example` functions, and the release it arrived in. The pages are generated from the Go source, so they match `go doc` for the same version.\n\n")
	b.WriteString("*Since* names the first release whose API listing (`api/*.txt`) has the identifier; " + "`" + nextVersion + "`" + " marks API added on this branch. What the tiers promise: [Stability](../guides/stability.md).\n\n")
	for _, group := range []struct {
		name string
		core bool
	}{{"Core", true}, {"Modules", false}} {
		fmt.Fprintf(&b, "## %s\n\n", group.name)
		if group.core {
			b.WriteString("The root module, `gorbital.dev`: small packages every app uses, with no dependencies beyond the standard library, OpenTelemetry and `golang.org/x`.\n\n")
		} else {
			b.WriteString("One Go module per directory under `modules/`, each added to an app on its own.\n\n")
		}
		b.WriteString("| Package | Summary |\n|---|---|\n")
		for _, p := range s.pkgs {
			if p.core() != group.core {
				continue
			}
			fmt.Fprintf(&b, "| [`%s`](%s.md) | %s |\n", p.importPath, p.slug, tableCell(p.doc.Synopsis(p.doc.Doc)))
		}
		b.WriteString("\n")
	}
	return trimTrailingBlank(b.Bytes())
}

// renderPackage writes one package page: import path, package doc, overlay,
// contents, then constants, variables, functions and types with their
// constructors and methods.
func (s *methodsSite) renderPackage(p *libPackage) []byte {
	r := &pkgRenderer{site: s, pkg: p}
	b := &r.b
	fmt.Fprintf(b, "# %s\n\n", p.title())
	b.WriteString(methodsNote)
	fmt.Fprintf(b, "```go\nimport %q\n```\n\n", p.importPath)
	if doc := r.markdown(p.doc.Doc, 2); doc != "" {
		b.WriteString(doc)
	}
	if overlay := bytes.TrimSpace(s.overlays[p.slug]); len(overlay) > 0 {
		b.Write(overlay)
		b.WriteString("\n\n")
	}
	r.examples(p.doc.Examples, "Example")
	r.contents()

	if len(p.doc.Consts) > 0 {
		b.WriteString("## Constants\n\n")
		r.values(p.doc.Consts)
	}
	if len(p.doc.Vars) > 0 {
		b.WriteString("## Variables\n\n")
		r.values(p.doc.Vars)
	}
	if len(p.doc.Funcs) > 0 {
		b.WriteString("## Functions\n\n")
		for _, f := range p.doc.Funcs {
			r.function(f, "###")
		}
	}
	if len(p.doc.Types) > 0 {
		b.WriteString("## Types\n\n")
		for _, t := range p.doc.Types {
			r.typ(t)
		}
	}
	return trimTrailingBlank(b.Bytes())
}

type pkgRenderer struct {
	site *methodsSite
	pkg  *libPackage
	b    bytes.Buffer
}

// contents lists every identifier with a link to its anchor.
func (r *pkgRenderer) contents() {
	d := r.pkg.doc
	var lines []string
	valueNames := func(vs []*doc.Value) []string {
		var names []string
		for _, v := range vs {
			names = append(names, v.Names...)
		}
		return names
	}
	links := func(names []string) string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = fmt.Sprintf("[`%s`](#%s)", n, n)
		}
		return strings.Join(out, ", ")
	}
	if names := valueNames(d.Consts); len(names) > 0 {
		lines = append(lines, "- Constants: "+links(names))
	}
	if names := valueNames(d.Vars); len(names) > 0 {
		lines = append(lines, "- Variables: "+links(names))
	}
	if len(d.Funcs) > 0 {
		var names []string
		for _, f := range d.Funcs {
			names = append(names, f.Name)
		}
		lines = append(lines, "- Functions: "+links(names))
	}
	if len(d.Types) > 0 {
		lines = append(lines, "- Types:")
		for _, t := range d.Types {
			line := fmt.Sprintf("  - [`%s`](#%s)", t.Name, t.Name)
			var members []string
			members = append(members, valueNames(t.Consts)...)
			members = append(members, valueNames(t.Vars)...)
			for _, f := range t.Funcs {
				members = append(members, f.Name)
			}
			for _, m := range t.Methods {
				members = append(members, anchorOf(m))
			}
			if len(members) > 0 {
				line += ": " + links(members)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return
	}
	r.b.WriteString("## Contents\n\n" + strings.Join(lines, "\n") + "\n\n")
}

// values writes a constant or variable group: anchors for each name, the
// declaration, its doc and Since.
func (r *pkgRenderer) values(vs []*doc.Value) {
	for _, v := range vs {
		for _, n := range v.Names {
			fmt.Fprintf(&r.b, "<a id=\"%s\"></a>\n", n)
		}
		r.b.WriteString("\n")
		r.code(v.Decl)
		if doc := r.markdown(v.Doc, 5); doc != "" {
			r.b.WriteString(doc)
		}
		r.sinceValues(v.Names)
	}
}

// function writes a function, constructor or method under a heading.
func (r *pkgRenderer) function(f *doc.Func, level string) {
	anchor := anchorOf(f)
	heading := "func " + f.Name
	if f.Recv != "" {
		heading = fmt.Sprintf("func (%s) %s", f.Recv, f.Name)
	}
	fmt.Fprintf(&r.b, "<a id=\"%s\"></a>\n\n%s %s\n\n", anchor, level, heading)
	r.code(f.Decl)
	if doc := r.markdown(f.Doc, 5); doc != "" {
		r.b.WriteString(doc)
	}
	r.sinceLine(r.site.since.of(r.pkg.importPath, anchor))
	r.examples(f.Examples, "Example")
}

// typ writes a type, then its constants, variables, constructors and
// methods, as go doc groups them.
func (r *pkgRenderer) typ(t *doc.Type) {
	fmt.Fprintf(&r.b, "<a id=\"%s\"></a>\n", t.Name)
	for _, m := range memberNames(t) {
		fmt.Fprintf(&r.b, "<a id=\"%s.%s\"></a>\n", t.Name, m)
	}
	fmt.Fprintf(&r.b, "\n### type %s\n\n", t.Name)
	r.code(t.Decl)
	if doc := r.markdown(t.Doc, 5); doc != "" {
		r.b.WriteString(doc)
	}
	r.sinceLine(r.site.since.of(r.pkg.importPath, t.Name))
	r.examples(t.Examples, "Example")
	r.values(t.Consts)
	r.values(t.Vars)
	for _, f := range t.Funcs {
		r.function(f, "####")
	}
	for _, m := range t.Methods {
		r.function(m, "####")
	}
}

// memberNames lists a type's exported struct fields or interface methods,
// so doc links such as [Config.Timeout] have an anchor on the type.
func memberNames(t *doc.Type) []string {
	var fields *ast.FieldList
	switch u := t.Decl.Specs[0].(*ast.TypeSpec).Type.(type) {
	case *ast.StructType:
		fields = u.Fields
	case *ast.InterfaceType:
		fields = u.Methods
	default:
		return nil
	}
	var names []string
	for _, f := range fields.List {
		for _, n := range f.Names {
			if n.IsExported() && !slices.Contains(names, n.Name) {
				names = append(names, n.Name)
			}
		}
		if len(f.Names) == 0 { // embedded: named by its type
			if name := embeddedName(f.Type); ast.IsExported(name) {
				names = append(names, name)
			}
		}
	}
	return names
}

// embeddedName is the field name of an embedded type: T for T, *T, pkg.T or
// T[P].
func embeddedName(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return embeddedName(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.IndexExpr:
		return embeddedName(x.X)
	case *ast.IndexListExpr:
		return embeddedName(x.X)
	}
	return ""
}

func (r *pkgRenderer) sinceLine(version string) {
	fmt.Fprintf(&r.b, "*Since `%s`*\n\n", version)
}

// sinceValues writes one Since for a group, or one per release when the
// names arrived in different releases.
func (r *pkgRenderer) sinceValues(names []string) {
	byVersion := map[string][]string{}
	var order []string
	for _, n := range names {
		v := r.site.since.of(r.pkg.importPath, n)
		if _, ok := byVersion[v]; !ok {
			order = append(order, v)
		}
		byVersion[v] = append(byVersion[v], n)
	}
	if len(order) == 1 {
		r.sinceLine(order[0])
		return
	}
	parts := make([]string, len(order))
	for i, v := range order {
		parts[i] = fmt.Sprintf("`%s`: %s", v, strings.Join(byVersion[v], ", "))
	}
	fmt.Fprintf(&r.b, "*Since %s*\n\n", strings.Join(parts, "; "))
}

// examples writes Example functions: their doc, code and expected output.
func (r *pkgRenderer) examples(exs []*doc.Example, label string) {
	for _, ex := range exs {
		title := label
		if ex.Suffix != "" {
			title += " (" + strings.ReplaceAll(ex.Suffix, "_", " ") + ")"
		}
		fmt.Fprintf(&r.b, "**%s**\n\n", title)
		if doc := r.markdown(ex.Doc, 5); doc != "" {
			r.b.WriteString(doc)
		}
		r.b.WriteString(fence("go", exampleCode(r.pkg.fset, ex)))
		if ex.Output != "" || ex.EmptyOutput {
			if ex.Unordered {
				r.b.WriteString("Output, in any order:\n\n")
			} else {
				r.b.WriteString("Output:\n\n")
			}
			r.b.WriteString(fence("text", ex.Output))
		}
	}
}

// code writes a declaration in a Go block.
func (r *pkgRenderer) code(decl ast.Decl) {
	r.b.WriteString(fence("go", formatDecl(r.pkg.fset, decl)))
}

// markdown converts a doc comment to Markdown with headings at level and doc
// links resolved to Methods anchors.
func (r *pkgRenderer) markdown(text string, level int) string {
	return docMarkdown(r.pkg, r.site.slugs(), text, level)
}

// slugs maps the import paths of documented packages to their page slugs.
func (s *methodsSite) slugs() map[string]string {
	m := make(map[string]string, len(s.pkgs))
	for _, p := range s.pkgs {
		m[p.importPath] = p.slug
	}
	return m
}

// docMarkdown converts Go doc comment text to Markdown. Code blocks become
// fenced blocks; [Name] and [pkg.Name] links point at the Methods page of a
// library package, or pkg.go.dev for anything else.
func docMarkdown(p *libPackage, slugs map[string]string, text string, level int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	parsed := p.doc.Parser().Parse(text)
	pr := p.doc.Printer()
	pr.HeadingLevel = level
	pr.HeadingID = func(*comment.Heading) string { return "" }
	pr.DocLinkURL = func(link *comment.DocLink) string {
		sym := link.Name
		if link.Recv != "" {
			sym = link.Recv + "." + link.Name
		}
		frag := ""
		if sym != "" {
			frag = "#" + sym
		}
		switch slug, ok := slugs[link.ImportPath]; {
		case link.ImportPath == "" || link.ImportPath == p.importPath:
			return frag
		case ok:
			return slug + ".md" + frag
		default:
			return "https://pkg.go.dev/" + link.ImportPath + frag
		}
	}
	var b strings.Builder
	for _, block := range parsed.Content {
		if code, ok := block.(*comment.Code); ok {
			lang := ""
			if isGo(code.Text) {
				lang = "go"
			}
			b.WriteString(fence(lang, code.Text))
			continue
		}
		b.Write(bytes.TrimRight(pr.Markdown(&comment.Doc{Content: []comment.Block{block}, Links: parsed.Links}), "\n"))
		b.WriteString("\n\n")
	}
	return b.String()
}

// isGo reports whether a code block in a doc comment is Go: declarations or
// statements that parse.
func isGo(code string) bool {
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", "package p\n"+code, 0); err == nil {
		return true
	}
	_, err := parser.ParseFile(fset, "", "package p\nfunc _() {\n"+code+"\n}", 0)
	return err == nil
}

// printConfig formats like gofmt.
var printConfig = printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}

// formatDecl prints a declaration without its doc comment or body. Field and
// spec comments stay; go/doc has already dropped unexported fields and
// methods, leaving a "contains filtered or unexported fields" comment.
func formatDecl(fset *token.FileSet, decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		c := *d
		c.Doc, c.Body = nil, nil
		decl = &c
	case *ast.GenDecl:
		c := *d
		c.Doc = nil
		decl = &c
	}
	var b bytes.Buffer
	if err := printConfig.Fprint(&b, fset, decl); err != nil {
		return fmt.Sprintf("// cannot print declaration: %v", err)
	}
	return b.String()
}

// outputComment starts the expected output at the end of an example.
var outputComment = regexp.MustCompile(`(?i)^\s*//\s*(unordered )?output:`)

// exampleCode prints an example's body without its braces and without the
// expected output comment, or the whole file for a whole-file example.
func exampleCode(fset *token.FileSet, ex *doc.Example) string {
	var b bytes.Buffer
	if err := printConfig.Fprint(&b, fset, &printer.CommentedNode{Node: ex.Code, Comments: ex.Comments}); err != nil {
		return fmt.Sprintf("// cannot print example: %v", err)
	}
	code := b.String()
	if _, isBlock := ex.Code.(*ast.BlockStmt); !isBlock {
		return code
	}
	code = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(code), "{"), "}")
	var lines []string
	for line := range strings.Lines(code) {
		line = strings.TrimRight(line, "\n")
		if outputComment.MatchString(line) {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "\t"))
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n") + "\n"
}

// fence wraps text in a fenced code block longer than any backtick run in it.
func fence(lang, text string) string {
	marker := "```"
	for strings.Contains(text, marker) {
		marker += "`"
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return marker + lang + "\n" + text + marker + "\n\n"
}

// tableCell makes text safe inside a Markdown table cell.
func tableCell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func trimTrailingBlank(b []byte) []byte {
	return append(bytes.TrimRight(b, "\n"), '\n')
}
