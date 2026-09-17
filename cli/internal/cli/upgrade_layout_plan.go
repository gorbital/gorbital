package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gorbital.dev/cli/internal/merge"
	"gorbital.dev/cli/internal/recipes"
)

// planBuiltinModules decides what happens to the generated code of the
// built-in modules: unchanged, the library runs it; changed, the app keeps
// it as its own code, copied from the library like orb eject with the app's
// changes moved into the copy.
func (m *layoutMove) planBuiltinModules(ctx context.Context) error {
	var customised []ejectableModule
	for _, e := range ejectableModules {
		prefix := e.dir() + "/"
		generated := pathsWithPrefix(m.base, prefix)
		if len(generated) == 0 {
			continue
		}
		changed, deleted, added := m.changedFiles(prefix)
		if len(changed)+len(deleted)+len(added) == 0 {
			m.delete(generated...)
			m.item(itemLibrary, e.dir(), fmt.Sprintf("%d generated files you never changed; %s runs them now", len(generated), e.importPath()), nil, nil)
			continue
		}
		customised = append(customised, e)
	}
	if len(customised) == 0 {
		return nil
	}
	owned := slices.Clone(customised)
	if slices.ContainsFunc(customised, func(e ejectableModule) bool { return e.name == "auth" }) {
		// The library's orgshttp takes sign-in's authenticator, so an app
		// that owns sign-in owns organisations too (ADR-0083).
		orgs, _ := lookupEjectable("orgs")
		if len(pathsWithPrefix(m.base, orgs.dir()+"/")) > 0 && !slices.ContainsFunc(owned, func(e ejectableModule) bool { return e.name == "orgs" }) {
			owned = append(owned, orgs)
		}
	}
	slices.SortFunc(owned, func(a, b ejectableModule) int { return strings.Compare(a.name, b.name) })

	// The library's own source, which the kept modules are copied from.
	lib, err := m.library(ctx)
	if err != nil {
		return err
	}
	for _, e := range owned {
		prefix := e.dir() + "/"
		keep, err := keepBuiltinModule(m.app, e, lib, owned, m.base, m.ours)
		if err != nil {
			return fmt.Errorf("keep the %s module as the app's code: %w", e.name, err)
		}
		changed, _, _ := m.changedFiles(prefix)
		if len(keep.Failed) > 0 {
			m.manual(e.dir(), fmt.Sprintf("changes to the generated %s module that don't fit the library's module", e.name), keep.Failed, changed...)
			m.delete(pathsWithPrefix(m.base, prefix)...)
			continue
		}
		for p, content := range keep.Files {
			m.write(p, content)
		}
		for p, content := range keep.Migrations {
			if _, ok := m.ours[p]; !ok {
				m.write(p, content)
			}
			m.handled[p] = true
		}
		for _, p := range pathsWithPrefix(m.base, prefix) {
			if _, kept := keep.Files[p]; !kept {
				m.delete(p)
			}
		}
		for _, p := range pathsWithPrefix(m.ours, prefix) {
			m.handled[p] = true
		}
		m.ejected = append(m.ejected, lockEjected{
			Module: e.name, Package: e.importPath(), Version: keep.Version, Date: m.now.Format("2006-01-02"), SHA256: keep.SHA256,
		})
		m.res.Ejected = append(m.res.Ejected, keep)
		detail := fmt.Sprintf("you changed it, so the app keeps it: the library's %s copied in as orb eject copies it, with your changes", e.importPath())
		if len(keep.Carried) == 0 {
			detail = fmt.Sprintf("you changed it, so the app keeps it: the library's %s copied in as orb eject copies it", e.importPath())
		}
		m.item(itemKept, e.dir(), detail, keep.Carried, keep.Hooks)
	}
	return nil
}

// library returns the gorbital.dev/gorbital module the app will build with:
// the checkout its go.mod replaces the library with, or the version the
// templates require, downloaded if the module cache doesn't have it.
func (m *layoutMove) library(ctx context.Context) (librarySource, error) {
	info, err := readGoMod(ctx, m.app.dir)
	if err != nil {
		return librarySource{}, err
	}
	if _, local := info.gorbital(); local != "" {
		dir := local
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(m.app.dir, dir)
		}
		dir = filepath.Join(dir, "gorbital")
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return librarySource{}, fmt.Errorf("the checkout %s has no gorbital directory: it is older than v0.2, and the v0.2 layout needs %s", local, gorbitalImportPath)
		}
		return librarySource{Version: "(local)", Dir: dir}, nil
	}
	version := templateLibraryVersion(m.target["go.mod"])
	var stderr bytes.Buffer
	out, err := goOutputIn(ctx, m.app.dir, &stderr, "mod", "download", "-json", gorbitalImportPath+"@"+version)
	if err != nil {
		return librarySource{}, fmt.Errorf("download %s@%s: %w: %s", gorbitalImportPath, version, err, strings.TrimSpace(stderr.String()))
	}
	var mod struct{ Version, Dir string }
	if err := json.Unmarshal(out, &mod); err != nil {
		return librarySource{}, fmt.Errorf("read go mod download's output: %w", err)
	}
	if mod.Dir == "" {
		return librarySource{}, fmt.Errorf("go mod download reports no source directory for %s@%s", gorbitalImportPath, version)
	}
	return librarySource{Version: mod.Version, Dir: mod.Dir}, nil
}

// templateLibraryVersion returns the gorbital.dev/gorbital version the v0.2
// templates require.
func templateLibraryVersion(goMod []byte) string {
	for line := range strings.Lines(string(goMod)) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == gorbitalImportPath {
			return fields[1]
		}
	}
	return recipes.LibraryVersion
}

// moduleLine matches the lines orb gen resource adds to internal/app, which
// the modules' conversion replaces: their registration and permissions.
var moduleLine = regexp.MustCompile(`^(register[A-Za-z0-9_]+\(api, mapper, svc\),|[a-z][A-Za-z0-9_]*Permissions,)$`)

// planAppModules converts the app's own modules — the example ones a new
// app gets and every module orb gen resource or the developer wrote.
func (m *layoutMove) planAppModules() {
	names := map[string]bool{}
	for _, files := range []map[string][]byte{m.base, m.ours} {
		for p := range files {
			rest, ok := strings.CutPrefix(p, "internal/modules/")
			if !ok {
				continue
			}
			name, _, ok := strings.Cut(rest, "/")
			if ok && !isBuiltinModule(name) {
				names[name] = true
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		m.planAppModule(name)
	}
}

func isBuiltinModule(name string) bool {
	_, ok := lookupEjectable(name)
	return ok
}

func (m *layoutMove) planAppModule(name string) {
	prefix := "internal/modules/" + name + "/"
	generated := pathsWithPrefix(m.base, prefix)
	changed, deleted, added := m.changedFiles(prefix)
	wiring := "internal/app/module_" + name + ".go"
	_, wiringGenerated := m.base[wiring]
	wiringChanged := wiringGenerated && !bytes.Equal(m.ours[wiring], m.base[wiring])

	untouched := len(generated) > 0 && len(changed)+len(deleted)+len(added) == 0 && !wiringChanged
	if len(generated) > 0 && len(deleted) == len(generated) && len(added) == 0 {
		// The developer removed the example module.
		m.delete(generated...)
		m.delete(wiring)
		for _, p := range pathsWithPrefix(m.target, prefix) {
			m.handled[p] = true
		}
		m.item(itemLibrary, prefix, "you removed this example module; it stays removed", nil, nil)
		return
	}

	module := map[string][]byte{}
	for _, p := range pathsWithPrefix(m.ours, prefix) {
		module[p] = m.ours[p]
	}
	appPkg := map[string][]byte{}
	for _, p := range pathsWithPrefix(m.ours, "internal/app/") {
		appPkg[p] = m.ours[p]
	}
	c := convertModule(name, m.app, module, appPkg, m.orgScoped(name))
	for _, p := range pathsWithPrefix(m.target, prefix) {
		m.handled[p] = true // the app's module stays; the template's doesn't replace it
	}
	if len(c.Problems) > 0 && untouched && len(pathsWithPrefix(m.target, prefix)) > 0 {
		// An example module nobody changed, whose v0.1 wiring has no
		// mechanical translation (the ping module's setting and flag were
		// declared in internal/app): the v0.2 templates have the same
		// module, with the same operations (ADR-0083).
		for _, p := range pathsWithPrefix(m.target, prefix) {
			m.write(p, m.target[p])
		}
		for _, p := range generated {
			if _, ok := m.target[p]; !ok {
				m.delete(p)
			}
		}
		for _, p := range pathsWithPrefix(m.ours, prefix) {
			if _, written := m.writes[p]; !written {
				m.delete(p)
			}
			m.handled[p] = true
		}
		m.delete(wiring)
		m.handled[wiring] = true
		m.item(itemTemplate, prefix, "the example module, unchanged: written from the v0.2 templates, which declare its settings and flags in the module itself",
			append([]string{
				"the template's module is orb gen module's output, so its request schemas may be renamed after their operations and may refuse unknown properties where the v0.1 module accepted them; often neither changes. Read git diff api/openapi.json, and restore the module with git and convert it by hand only if a schema your clients depend on did change",
				"orb upgrade couldn't convert the module where it was:",
			}, c.Problems...), nil)
		return
	}
	if len(c.Problems) > 0 {
		keep := append(slices.Clone(changed), added...)
		if c.Wiring != "" {
			keep = append(keep, c.Wiring)
		}
		m.manual(prefix, "orb upgrade can't convert this module", c.Problems, keep...)
		for _, p := range pathsWithPrefix(m.ours, prefix) {
			m.handled[p] = true // left as it is, for the developer to finish
		}
		if c.Wiring != "" {
			m.handled[c.Wiring] = true
			m.preserve(c.Wiring)
			m.delete(c.Wiring)
		}
		return
	}
	for p, content := range c.Files {
		m.write(p, content)
	}
	for _, p := range pathsWithPrefix(m.ours, prefix) {
		m.handled[p] = true
	}
	if c.Wiring != "" {
		m.delete(c.Wiring)
		m.handled[c.Wiring] = true
	}
	notes := slices.Clone(c.Notes)
	if len(c.Routes) > 0 {
		notes = append(notes, fmt.Sprintf("%d operations are gorbital routes now: %s", len(c.Routes), strings.Join(c.Routes, "; ")))
	}
	if len(c.Escaped) > 0 {
		notes = append(notes, fmt.Sprintf("%d operations keep their huma.Operation value, registered with operation.Register: %s", len(c.Escaped), strings.Join(c.Escaped, "; ")))
	}
	if len(c.Public) > 0 {
		notes = append(notes, "public routes (no security requirement in v0.1), now guard.Public(): "+strings.Join(c.Public, ", "))
	}
	if len(c.Routes) > len(c.Public) {
		notes = append(notes, "routes that needed a token now deny unauthenticated requests before the input is validated; the use cases' own permission checks are unchanged, and can become guard.Permission")
	}
	if c.Errors > 0 {
		notes = append(notes, fmt.Sprintf("%d error mappings moved from %s into Module.Errors", c.Errors, c.Wiring))
	}
	if len(c.Permissions) > 0 {
		notes = append(notes, "permissions moved into Module.Permissions: "+strings.Join(c.Permissions, ", "))
	}
	m.res.Modules = append(m.res.Modules, c)
	m.item(itemConverted, prefix, "the app's module: routes, errors and permissions in its Module value", notes, nil)
}

// orgScoped reports whether the app gives a module's permissions to the
// organisation roles, as orb gen resource --scope org does. It reads the
// whole list under the anchor: a module the list names but that this misses
// has its permissions regraded to the platform "user" role, which every
// signed-in account holds, instead of the organisation's roles.
func (m *layoutMove) orgScoped(name string) bool {
	src, ok := m.ours["internal/app/permissions.go"]
	if !ok {
		return false
	}
	after := sectionAfter(string(src), recipes.OrgPermissionsAnchor)
	if after == "" {
		return false
	}
	return slices.Contains(listEntries(after), name+"Permissions,")
}

// listEntries returns the trimmed entries of the composite literal that
// follows, up to the line closing it.
func listEntries(s string) []string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "}" || line == "})" {
			break
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// sectionAfter returns the text after the first line containing anchor.
func sectionAfter(src, anchor string) string {
	i := strings.Index(src, anchor)
	if i < 0 {
		return ""
	}
	return src[i+len(anchor):]
}

// planCompositionRoot decides what happens to internal/app, internal/jobs,
// cmd/migrate and cmd/seed: the v0.2 layout has none of them.
func (m *layoutMove) planCompositionRoot() {
	m.planMiddleware()
	generated := 0
	for _, prefix := range compositionRoot {
		for _, p := range pathsWithPrefix(m.base, prefix) {
			if m.handled[p] {
				continue
			}
			current, ok := m.ours[p]
			switch {
			case !ok:
				m.handled[p] = true
			case bytes.Equal(current, m.base[p]):
				m.delete(p)
				generated++
			case p == "internal/app/modules.go" || p == "internal/app/permissions.go":
				if extra := changedLinesOutside(m.base[p], current, moduleLine); len(extra) > 0 {
					m.manual(p, "changed outside the lines orb gen resource adds", extra, p)
				}
				m.delete(p)
			case strings.HasSuffix(p, "_test.go"):
				m.followUp(p, "a generated test of the v0.1 composition root; the library's own tests cover what it tested, and a new app's tests are in cmd/api and internal/modules", p)
				m.delete(p)
			default:
				m.manual(p, "generated code you changed; the library runs it in the v0.2 layout, so the change has to move into main.go's options, a module or a hook", nil, p)
				m.delete(p)
			}
		}
	}
	for _, prefix := range compositionRoot {
		for _, p := range pathsWithPrefix(m.ours, prefix) {
			if m.handled[p] {
				continue
			}
			if _, fromTemplate := m.base[p]; fromTemplate {
				continue
			}
			switch {
			case strings.HasPrefix(p, "internal/jobs/"):
				m.manual(p, "a job package of yours: declare it in a module's Jobs (gorbital.Module) and delete it here; it stays where it is, unwired", nil)
				m.handled[p] = true
			case strings.HasSuffix(p, "_test.go"):
				m.followUp(p, "a test of the v0.1 composition root; port it to cmd/api or the module it tests (gorbitaltest)", p)
				m.delete(p)
			default:
				m.manual(p, "your own code in the composition root; move it into a module, main.go or a hook", nil, p)
				m.delete(p)
			}
		}
	}
	if generated > 0 {
		m.item(itemLibrary, "internal/app, internal/jobs, cmd/migrate, cmd/seed", fmt.Sprintf("%d generated files you never changed; gorbital.Main and the built-in modules run them now", generated), nil, nil)
	}
}

// planMiddleware carries the app's own middleware from routes.go into
// main.go's options.
func (m *layoutMove) planMiddleware() {
	current, ok := m.ours[routesPath]
	if !ok || bytes.Equal(current, m.base[routesPath]) {
		return
	}
	decls := map[string]bool{}
	for _, p := range pathsWithPrefix(m.target, "cmd/api/") {
		if g, err := parseGoSource(p, m.target[p]); err == nil {
			for _, name := range fileDeclarations(g) {
				decls[name] = true
			}
		}
	}
	carry, err := carryMiddleware(m.app, m.base, m.ours, decls)
	if err != nil {
		m.manual(routesPath, "orb upgrade can't read the app's middleware: "+err.Error(), nil, routesPath)
		m.delete(routesPath)
		return
	}
	if len(carry.Problems) > 0 {
		m.manual(routesPath, "the app's middleware can't be carried over", carry.Problems, routesPath)
		m.delete(routesPath)
		return
	}
	for p, content := range carry.Files {
		m.write(p, content)
	}
	for _, moved := range carry.Moved {
		from, _, _ := strings.Cut(moved, " → ")
		m.delete(from)
		m.handled[from] = true
	}
	m.delete(routesPath)
	m.res.Middleware = &carry
	notes := []string{}
	if len(carry.Added) > 0 {
		notes = append(notes, "middleware: "+strings.Join(carry.Added, ", "))
	}
	if len(carry.Moved) > 0 {
		notes = append(notes, "moved: "+strings.Join(carry.Moved, ", "))
	}
	if len(carry.Removed) > 0 {
		notes = append(notes, "steps your stack left out, left out again: "+strings.Join(carry.Removed, ", "))
	}
	detail := "the stack of internal/app/routes.go, as the default stack"
	if carry.Option != "" {
		detail = "the app's middleware, through " + carry.Option
	}
	m.item(itemCarried, routesPath, detail, notes, nil)
}

// changedLinesOutside returns the lines that differ between base and ours
// and don't match allowed.
func changedLinesOutside(base, ours []byte, allowed *regexp.Regexp) []string {
	var extra []string
	for _, e := range lineEdits(splitFileLines(base), splitFileLines(ours)) {
		if e.op == ' ' || allowed.MatchString(strings.TrimSpace(e.line)) {
			continue
		}
		extra = append(extra, string(e.op)+" "+strings.TrimSpace(e.line))
	}
	return extra
}

// planMigrations keeps the app's migration history exactly as it is. The
// app's copies of the library's migrations stay: gorbital.Migrate treats a
// copy with the same version and identical content as the same migration
// (ADR-0083 §6), the database sees nothing new, and the module tests that
// migrate with db/migrations keep working. Deleting them would leave those
// tests without the tables the app's own migrations reference.
func (m *layoutMove) planMigrations() {
	kept := 0
	for _, p := range pathsWithPrefix(m.base, "db/migrations/") {
		if m.handled[p] || !strings.HasSuffix(p, ".sql") {
			continue
		}
		current, ok := m.ours[p]
		switch {
		case !ok:
			m.handled[p] = true
		case !bytes.Equal(current, m.base[p]):
			m.manual(p, "a copy of a library migration you changed; gorbital.Migrate refuses a file that differs from the module's own, so give your change a new version", nil)
			m.handled[p] = true
		case m.target[p] != nil:
			m.handled[p] = true // the app's own migration, in both layouts
		default:
			m.handled[p] = true
			kept++
		}
	}
	// The row-level security policy a multi-tenant app carries is a template
	// orb add rls copies into db/migrations; the v0.2 templates have none,
	// and deleting it would take the app's copy away.
	if _, ok := m.ours[recipes.RowLevelSecurityPath]; ok {
		m.handled[recipes.RowLevelSecurityPath] = true
		m.item(itemTemplate, recipes.RowLevelSecurityPath, "kept as it is: orb add rls copies it into db/migrations when you turn row-level security on", nil, nil)
	}
	if kept > 0 {
		m.item(itemLibrary, "db/migrations", fmt.Sprintf("%d copies of the library's migrations kept as they are; the library declares the same versions, and gorbital.Migrate reads an identical copy as the same migration, so the database sees nothing new", kept),
			[]string{"a new app has only its own migrations there; you can delete the copies once nothing else reads them (the module tests that migrate with db/migrations do)"}, nil)
	}
}

// planGoFiles writes the v0.2 templates' Go files and removes the v0.1
// ones, outside the modules and the composition root.
func (m *layoutMove) planGoFiles() {
	paths := map[string]bool{}
	for _, files := range []map[string][]byte{m.base, m.target} {
		for p := range files {
			if strings.HasSuffix(p, ".go") && !m.handled[p] {
				paths[p] = true
			}
		}
	}
	for _, p := range slices.Sorted(maps.Keys(paths)) {
		template, inTemplate := m.target[p]
		generated, wasGenerated := m.base[p]
		current, hasOurs := m.ours[p]
		switch {
		case inTemplate && !hasOurs:
			m.write(p, template)
		case inTemplate && wasGenerated && bytes.Equal(current, generated):
			m.write(p, template)
		case inTemplate:
			m.manual(p, "the v0.2 templates write this file and yours differs; the template's version is written and yours kept in "+layoutPreservedDir+"/", nil, p)
			m.write(p, template)
		case wasGenerated && hasOurs && bytes.Equal(current, generated):
			m.delete(p)
		case wasGenerated && hasOurs:
			m.manual(p, "generated code you changed, which the v0.2 layout doesn't have", nil, p)
			m.delete(p)
		default:
			m.handled[p] = true
		}
	}
}

// planOtherFiles merges everything that isn't Go code or a migration —
// README, Dockerfile, compose.yaml, .env.example, gorbital.yaml — the way
// orb upgrade merges templates.
func (m *layoutMove) planOtherFiles(ctx context.Context) error {
	base, theirs := map[string][]byte{}, map[string][]byte{}
	skip := func(p string) bool {
		return m.handled[p] || strings.HasSuffix(p, ".go") || strings.HasPrefix(p, "db/migrations/") ||
			slices.Contains(untrackedPaths, p) || slices.Contains(derivedPaths, p) || p == lockPath
	}
	for p, content := range m.base {
		if !skip(p) {
			base[p] = content
		}
	}
	for p, content := range m.target {
		if !skip(p) {
			theirs[p] = content
		}
	}
	changes, err := merge.Plan(ctx, merge.Input{
		Base: base, Theirs: theirs, Unproven: map[string]bool{},
		Ours:  func(p string) ([]byte, bool, error) { content, ok := m.ours[p]; return content, ok, nil },
		Label: "gorbital " + Version + " (v0.2 layout)",
	})
	if err != nil {
		return err
	}
	for _, c := range changes {
		switch c.Action {
		case merge.Create, merge.Update, merge.Merged, merge.Conflict:
			m.write(c.Path, c.Content)
			if c.Action == merge.Conflict {
				m.res.Conflicts = append(m.res.Conflicts, c.Path)
			}
		case merge.Delete:
			m.delete(c.Path)
		default:
			m.handled[c.Path] = true
		}
	}
	return nil
}

// pathsWithPrefix returns the files under prefix, sorted.
func pathsWithPrefix(files map[string][]byte, prefix string) []string {
	var out []string
	for p := range files {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// finish turns the planned files into the result: main.go's options, the
// lock, the report and the list of changes.
func (m *layoutMove) finish() (*layoutPlan, error) {
	if len(m.ejected) > 0 {
		owned := make([]ejectableModule, 0, len(m.ejected))
		for _, e := range m.ejected {
			if mod, ok := lookupEjectable(e.Module); ok {
				owned = append(owned, mod)
			}
		}
		rewrite := ejectImportRewriter(m.app.module, owned)
		for _, p := range slices.Sorted(maps.Keys(m.writes)) {
			if !strings.HasSuffix(p, ".go") || strings.HasPrefix(p, layoutPreservedDir+"/") || strings.HasPrefix(p, "internal/modules/") {
				continue
			}
			content, changed, err := rewriteGoImports(p, m.writes[p], rewrite)
			if err != nil {
				return nil, err
			}
			if changed {
				m.writes[p] = content
			}
		}
	}
	if m.res.Middleware != nil && m.res.Middleware.Option != "" {
		main, ok := m.writes["cmd/api/main.go"]
		if !ok {
			return nil, errors.New("cmd/api/main.go isn't written, so the app's middleware can't be added to its options")
		}
		content, err := addMainOption(main, m.res.Middleware.Option, m.res.Middleware.Comment)
		if err != nil {
			return nil, err
		}
		m.writes["cmd/api/main.go"] = content
	}

	lock := lockFromTree(m.inputs, m.lockTree())
	lock.Ejected = m.ejected
	encoded, err := lock.encode()
	if err != nil {
		return nil, err
	}
	m.writes[lockPath] = encoded

	m.res.Next = []string{
		"go test ./...   (database tests need orb dev or docker compose up -d --wait)",
		"git diff api/openapi.json   (re-exported: every route gains x-gorbital-guards, and the /ops instance example takes this app's name)",
		"git diff api/surface.json api/openapi.baseline.json   (rewritten: the built-in modules' names are the library's now, so only this app's are recorded)",
		"orb doctor",
		"git add -A && git commit -m 'Move to the v0.2 layout'",
	}
	m.writes[upgradeReportPath] = m.report()
	m.res.Changes = m.changes()
	plan := &layoutPlan{res: m.res, writes: m.writes, deletes: m.deletes, goMod: m.target["go.mod"], ejected: m.ejected, appDir: m.app.dir}
	return plan, nil
}

// lockTree is what orb wrote into the app, for gorbital.lock: the v0.2
// templates, without the files the app's own code replaces.
func (m *layoutMove) lockTree() map[string][]byte {
	tree := map[string][]byte{}
	for p, content := range m.target {
		if _, written := m.writes[p]; !written {
			continue
		}
		tree[p] = content
	}
	return tree
}

// changes lists every file the move writes or deletes, sorted by path.
func (m *layoutMove) changes() []layoutChange {
	var changes []layoutChange
	for _, p := range slices.Sorted(maps.Keys(m.writes)) {
		action := "create"
		before := m.ours[p]
		if _, ok := m.ours[p]; ok {
			action = "update"
			if bytes.Equal(before, m.writes[p]) {
				continue
			}
		}
		if slices.Contains(m.res.Conflicts, p) {
			action = "conflict"
		}
		changes = append(changes, layoutChange{Path: p, Action: action, content: m.writes[p], before: before})
	}
	for _, p := range m.deletes {
		changes = append(changes, layoutChange{Path: p, Action: "delete", before: m.ours[p]})
	}
	slices.SortFunc(changes, func(a, b layoutChange) int { return strings.Compare(a.Path, b.Path) })
	return changes
}
