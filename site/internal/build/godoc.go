package build

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/token"
	"html"
	"html/template"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// packageSkipDirs are never scanned for library packages: internal packages
// aren't public API, and the rest aren't the library.
var packageSkipDirs = map[string]bool{
	"internal": true, "testdata": true, "examples": true, "cli": true, "site": true,
	"spikes": true, "scripts": true, "docs": true, "dist": true, "node_modules": true,
}

type goPackage struct {
	importPath string
	dir        string // repository path of the package directory
	doc        *doc.Package
	fset       *token.FileSet
	comments   []*ast.CommentGroup
	anchors    map[string]bool
	page       *page
}

// collectPackages adds the package reference: an index and a page for each
// public package of the library, rendered from its Go doc comments, so the
// reference always matches the code.
func (b *builder) collectPackages(ti int, g navGroup) error {
	pkgs, err := b.loadPackages()
	if err != nil {
		return err
	}
	slices.SortStableFunc(pkgs, func(x, y *goPackage) int {
		gx, _ := packageGroup(x)
		gy, _ := packageGroup(y)
		return cmp.Or(cmp.Compare(gx, gy), strings.Compare(x.dir, y.dir))
	})
	index := &page{URL: pageURL(g.Slug), Title: g.Name, Label: "All packages", Tab: ti, Group: g.Name, Kind: "package"}
	b.add(index)
	byPath := map[string]*goPackage{}
	for _, gp := range pkgs {
		_, group := packageGroup(gp)
		title := strings.TrimPrefix(gp.importPath, "apistock.dev/")
		gp.page = &page{URL: pageURL(path.Join(g.Slug, gp.dir)), Title: title, Label: title, Tab: ti, Group: group, Kind: "package"}
		gp.anchors = packageAnchors(gp.doc)
		byPath[gp.importPath] = gp
		b.add(gp.page)
	}
	for _, gp := range pkgs {
		b.renderPackage(gp, byPath)
	}
	renderPackageIndex(index, pkgs)
	return nil
}

// packageGroup orders and names the sidebar groups of the reference.
func packageGroup(gp *goPackage) (int, string) {
	switch {
	case strings.HasSuffix(gp.doc.Name, "test"):
		return 2, "Test helpers"
	case strings.HasPrefix(gp.dir, "modules/"):
		return 1, "Module packages"
	}
	return 0, "Core packages"
}

type goModule struct {
	dir, path string
}

// loadPackages finds every non-internal, non-main package in the library's
// modules and reads its documentation.
func (b *builder) loadPackages() ([]*goPackage, error) {
	var modules []goModule
	files := map[string][]string{}
	err := filepath.WalkDir(b.cfg.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(b.cfg.Root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if d.IsDir() {
			if rel != "." && (packageSkipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case name == "go.mod":
			mp, err := modulePath(p)
			if err != nil {
				return err
			}
			modules = append(modules, goModule{dir: path.Dir(rel), path: mp})
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			files[path.Dir(rel)] = append(files[path.Dir(rel)], p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("build: package reference: %w", err)
	}

	var pkgs []*goPackage
	for _, dir := range slices.Sorted(maps.Keys(files)) {
		mod, ok := owningModule(modules, dir)
		if !ok {
			continue
		}
		sub := dir
		if mod.dir != "." {
			sub = strings.TrimPrefix(strings.TrimPrefix(dir, mod.dir), "/")
		}
		importPath := mod.path
		if sub != "" && sub != "." {
			importPath += "/" + sub
		}
		gp, err := parsePackage(dir, importPath, files[dir])
		if err != nil {
			return nil, err
		}
		if gp != nil {
			pkgs = append(pkgs, gp)
		}
	}
	return pkgs, nil
}

// owningModule returns the innermost module containing dir.
func owningModule(modules []goModule, dir string) (goModule, bool) {
	best, depth := goModule{}, -1
	for _, m := range modules {
		d := 0
		switch {
		case m.dir == ".":
		case dir == m.dir || strings.HasPrefix(dir, m.dir+"/"):
			d = len(m.dir)
		default:
			continue
		}
		if d > depth {
			best, depth = m, d
		}
	}
	return best, depth >= 0
}

func parsePackage(dir, importPath string, paths []string) (*goPackage, error) {
	fset := token.NewFileSet()
	byName := map[string][]*ast.File{}
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("build: package reference: %w", err)
		}
		byName[f.Name.Name] = append(byName[f.Name.Name], f)
	}
	// A directory can hold a generator in package main behind a build tag;
	// the library package is the largest non-main one.
	name := ""
	for n, fs := range byName {
		if n != "main" && (name == "" || len(fs) > len(byName[name])) {
			name = n
		}
	}
	if name == "" {
		return nil, nil
	}
	astFiles := byName[name]
	dp, err := doc.NewFromFiles(fset, astFiles, importPath)
	if err != nil {
		return nil, fmt.Errorf("build: package reference: %s: %w", importPath, err)
	}
	gp := &goPackage{importPath: importPath, dir: dir, doc: dp, fset: fset}
	for _, f := range astFiles {
		gp.comments = append(gp.comments, f.Comments...)
	}
	return gp, nil
}

func modulePath(gomod string) (string, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(data)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("%s: no module line", gomod)
}

// packageAnchors lists the names a package page has anchors for: exported
// constants, variables, functions, types and Type.Method.
func packageAnchors(p *doc.Package) map[string]bool {
	a := map[string]bool{}
	values := func(vs []*doc.Value) {
		for _, v := range vs {
			for _, n := range v.Names {
				if token.IsExported(n) {
					a[n] = true
				}
			}
		}
	}
	values(p.Consts)
	values(p.Vars)
	for _, f := range p.Funcs {
		a[f.Name] = true
	}
	for _, t := range p.Types {
		a[t.Name] = true
		values(t.Consts)
		values(t.Vars)
		for _, f := range t.Funcs {
			a[f.Name] = true
		}
		for _, m := range t.Methods {
			a[t.Name+"."+m.Name] = true
		}
	}
	return a
}

// docLinkURL resolves a [Name] or [pkg.Name] link in a doc comment: to an
// anchor on this site when the symbol has one, and to pkg.go.dev for other
// packages, such as the standard library.
func docLinkURL(from *goPackage, l *comment.DocLink, all map[string]*goPackage) string {
	target := from
	if l.ImportPath != "" && l.ImportPath != from.importPath {
		t, ok := all[l.ImportPath]
		if !ok {
			u := "https://pkg.go.dev/" + l.ImportPath
			if l.Name != "" {
				u += "#" + cmp.Or(strings.Trim(l.Recv+"."+l.Name, "."), l.Name)
			}
			return u
		}
		target = t
	}
	base := ""
	if target != from {
		base = target.page.URL
	}
	name := l.Name
	if l.Recv != "" {
		name = l.Recv + "." + l.Name
	}
	switch {
	case name != "" && target.anchors[name]:
		return base + "#" + name
	case l.Recv != "" && target.anchors[l.Recv]:
		return base + "#" + l.Recv
	}
	return target.page.URL
}

// renderPackage writes a package page: its overview, then its constants,
// variables, functions and types with their methods.
func (b *builder) renderPackage(gp *goPackage, all map[string]*goPackage) {
	p := gp.doc
	pr := p.Printer()
	pr.HeadingLevel = 3
	pr.DocLinkURL = func(l *comment.DocLink) string { return docLinkURL(gp, l, all) }
	parser := p.Parser()
	docHTML := func(text string) string { return string(pr.HTML(parser.Parse(text))) }
	docMD := func(text string) string { return string(pr.Markdown(parser.Parse(text))) }

	var body, md strings.Builder
	var toc []heading
	fmt.Fprintf(&body, "<div class=\"pkg-meta\"><code>import %s</code><a href=\"%s/tree/main/%s\">Source on GitHub</a></div>\n",
		html.EscapeString(strconv.Quote(gp.importPath)), b.site.GitHub, gp.dir)
	fmt.Fprintf(&md, "# %s\n\n```go\nimport %q\n```\n\n", gp.page.Title, gp.importPath)
	body.WriteString(docHTML(p.Doc))
	md.WriteString(docMD(p.Doc) + "\n")

	section := func(id, title string) {
		toc = append(toc, heading{ID: id, Text: title, Level: 2})
		fmt.Fprintf(&body, "<h2 id=\"%s\">%s<a class=\"anchor\" href=\"#%s\" aria-label=\"Link to this section\">#</a></h2>\n", id, title, id)
		fmt.Fprintf(&md, "## %s\n\n", title)
	}
	item := func(level int, id, label string, extraIDs []string, node ast.Node, text string) {
		fmt.Fprintf(&body, "<h%d id=\"%s\" class=\"decl-name\">%s", level, id, html.EscapeString(label))
		for _, x := range extraIDs {
			fmt.Fprintf(&body, "<span id=\"%s\"></span>", x)
		}
		fmt.Fprintf(&body, "<a class=\"anchor\" href=\"#%s\" aria-label=\"Link to %s\">#</a></h%d>\n", id, html.EscapeString(label), level)
		src := gp.format(node)
		body.WriteString(`<div class="code" data-lang="go"><div class="code-head"><span class="fname">go</span><button type="button" class="copy">Copy</button></div><pre><code>`)
		var hl bytes.Buffer
		highlight(&hl, "go", src)
		body.Write(hl.Bytes())
		body.WriteString("</code></pre></div>\n")
		if strings.TrimSpace(text) != "" {
			body.WriteString(docHTML(text))
		}
		fmt.Fprintf(&md, "%s %s\n\n```go\n%s\n```\n\n%s\n", strings.Repeat("#", level), label, src, docMD(text))
	}
	values := func(level int, vs []*doc.Value) {
		for _, v := range vs {
			names := slices.DeleteFunc(slices.Clone(v.Names), func(n string) bool { return !token.IsExported(n) })
			if len(names) == 0 {
				continue
			}
			label := v.Decl.Tok.String() + " " + names[0]
			if len(names) > 1 {
				label += ", …"
			}
			item(level, names[0], label, names[1:], v.Decl, v.Doc)
		}
	}

	if len(p.Consts) > 0 {
		section("constants", "Constants")
		values(3, p.Consts)
	}
	if len(p.Vars) > 0 {
		section("variables", "Variables")
		values(3, p.Vars)
	}
	if len(p.Funcs) > 0 {
		section("functions", "Functions")
		for _, f := range p.Funcs {
			item(3, f.Name, "func "+f.Name, nil, f.Decl, f.Doc)
		}
	}
	if len(p.Types) > 0 {
		section("types", "Types")
		for _, t := range p.Types {
			toc = append(toc, heading{ID: t.Name, Text: t.Name, Level: 3})
			item(3, t.Name, "type "+t.Name, nil, t.Decl, t.Doc)
			values(4, t.Consts)
			values(4, t.Vars)
			for _, f := range t.Funcs {
				item(4, f.Name, "func "+f.Name, nil, f.Decl, f.Doc)
			}
			for _, m := range t.Methods {
				item(4, t.Name+"."+m.Name, "func ("+m.Recv+") "+m.Name, nil, m.Decl, m.Doc)
			}
		}
	}

	synopsis := p.Synopsis(p.Doc)
	gp.page.Body = template.HTML(body.String()) //nolint:gosec // doc comments rendered and escaped by go/doc/comment
	gp.page.TOC = toc
	gp.page.Markdown = []byte(md.String())
	gp.page.Desc = summarize(synopsis)
	gp.page.text = synopsis + " " + strings.Join(slices.Sorted(maps.Keys(gp.anchors)), " ")
}

// format prints a declaration as gofmt would, with the comments inside it
// but without its doc comment, which the page shows as prose.
func (gp *goPackage) format(node ast.Node) string {
	switch d := node.(type) {
	case *ast.FuncDecl:
		d.Doc = nil
	case *ast.GenDecl:
		d.Doc = nil
	}
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 4}
	if err := cfg.Fprint(&buf, gp.fset, &printer.CommentedNode{Node: node, Comments: gp.comments}); err != nil {
		return ""
	}
	return buf.String()
}

var nonAnchor = regexp.MustCompile(`[^a-z0-9]+`)

func renderPackageIndex(index *page, pkgs []*goPackage) {
	var body, md strings.Builder
	intro := "Every public package of the apistock library, generated from its Go doc comments on each build, so it always matches the code. " +
		"Each page shows the package's constants, variables, functions and types with their documentation. Generated apps import these packages; test helpers are for your tests."
	fmt.Fprintf(&body, "<p>%s</p>\n", html.EscapeString(intro))
	fmt.Fprintf(&md, "# %s\n\n%s\n", index.Title, intro)
	current := ""
	for _, gp := range pkgs {
		_, group := packageGroup(gp)
		if group != current {
			if current != "" {
				body.WriteString("</tbody></table></div>\n")
			}
			current = group
			id := strings.Trim(nonAnchor.ReplaceAllString(strings.ToLower(group), "-"), "-")
			index.TOC = append(index.TOC, heading{ID: id, Text: group, Level: 2})
			fmt.Fprintf(&body, "<h2 id=\"%s\">%s<a class=\"anchor\" href=\"#%s\" aria-label=\"Link to this section\">#</a></h2>\n", id, group, id)
			body.WriteString("<div class=\"table-wrap\"><table><thead><tr><th>Package</th><th>What it does</th></tr></thead><tbody>\n")
			fmt.Fprintf(&md, "\n## %s\n\n| Package | What it does |\n|---|---|\n", group)
		}
		synopsis := gp.doc.Synopsis(gp.doc.Doc)
		fmt.Fprintf(&body, "<tr><td><a href=\"%s\"><code>%s</code></a></td><td>%s</td></tr>\n", gp.page.URL, html.EscapeString(gp.page.Title), html.EscapeString(synopsis))
		fmt.Fprintf(&md, "| [%s](%s) | %s |\n", gp.page.Title, mdPath(gp.page.URL), strings.ReplaceAll(synopsis, "|", `\|`))
	}
	if current != "" {
		body.WriteString("</tbody></table></div>\n")
	}
	index.Body = template.HTML(body.String()) //nolint:gosec // escaped above
	index.Markdown = []byte(md.String())
	index.Desc = summarize(intro)
	index.text = intro
}
