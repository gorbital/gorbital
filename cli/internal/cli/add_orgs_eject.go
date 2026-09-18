package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/merge"
)

// ejectOrgsWithAuth keeps orb add orgs building in an app that holds its
// sign-in, as every app orb new writes since v0.2.1 does: the library's
// orgshttp takes the library's sign-in, so the organisations module is
// copied into internal/modules too, as orb eject orgs copies it, and the
// merged files import the app's copies. It returns changes and the next
// gorbital.lock unchanged when the app's sign-in is the library's, or its
// organisations are already its own.
func ejectOrgsWithAuth(ctx context.Context, app appInfo, lock lockFile, changes []merge.Change, next lockFile) ([]merge.Change, lockFile, error) {
	_, authOwned := lock.ejected("auth")
	_, orgsOwned := lock.ejected("orgs")
	if !authOwned || orgsOwned {
		return changes, next, nil
	}
	m, _ := lookupEjectable("orgs")
	lib, err := goModLibrary(ctx, app.dir)
	if err != nil {
		return nil, next, fmt.Errorf("copy the organisations module next to the app's sign-in: %w", err)
	}
	plan, err := planEjectFrom(app, m, lib, lock, true, time.Now())
	if err != nil {
		return nil, next, err
	}

	// The merged files import the library's orgshttp (and sign-in, in
	// tests the multi-tenant tree adds): point them at the app's copies.
	var owned []ejectableModule
	for _, e := range lock.Ejected {
		if o, ok := lookupEjectable(e.Module); ok {
			owned = append(owned, o)
		}
	}
	rewrite := ejectImportRewriter(app.module, append(owned, m))
	out := make([]merge.Change, 0, len(changes))
	for _, c := range changes {
		if strings.HasSuffix(c.Path, ".go") && c.Content != nil {
			content, changed, err := rewriteGoImports(c.Path, c.Content, rewrite)
			if err != nil {
				return nil, next, err
			}
			if changed {
				c.Content = content
			}
		}
		out = append(out, c)
	}
	for _, c := range plan.Changes {
		if c.Kind == genplan.Create && c.Path != lockPath {
			out = append(out, merge.Change{Path: c.Path, Action: merge.Create, Content: c.Content})
		}
	}
	e, ok := planLockEntry(plan, m.name)
	if !ok {
		return nil, next, fmt.Errorf("orb eject's plan for %s records no ejection", m.name)
	}
	next.Ejected = append(slices.Clone(next.Ejected), e)
	slices.SortFunc(next.Ejected, func(a, b lockEjected) int { return strings.Compare(a.Module, b.Module) })
	return out, next, nil
}

// planLockEntry returns the gorbital.lock entry an eject plan records for
// module.
func planLockEntry(plan genplan.Plan, module string) (lockEjected, bool) {
	for _, c := range plan.Changes {
		if c.Path != lockPath {
			continue
		}
		var l lockFile
		if err := json.Unmarshal(c.Content, &l); err != nil {
			return lockEjected{}, false
		}
		return l.ejected(module)
	}
	return lockEjected{}, false
}

// goModLibrary returns the gorbital.dev/gorbital the app in dir builds
// with, from go.mod alone: its replace directive's directory, or the
// version it requires from the module cache. Unlike go list, it needs no
// go.sum.
func goModLibrary(ctx context.Context, dir string) (librarySource, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // the app's go.mod
	if err != nil {
		return librarySource{}, err
	}
	version, replaced := "", ""
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(line), "require "), "replace "))
		if len(fields) < 2 || fields[0] != gorbitalImportPath {
			continue
		}
		if fields[1] == "=>" && len(fields) >= 3 {
			replaced = fields[2]
		} else if strings.HasPrefix(fields[1], "v") {
			version = fields[1]
		}
	}
	switch {
	case version == "":
		return librarySource{}, fmt.Errorf("go.mod doesn't require %s", gorbitalImportPath)
	case replaced != "" && (strings.HasPrefix(replaced, "/") || strings.HasPrefix(replaced, ".")):
		if !filepath.IsAbs(replaced) {
			replaced = filepath.Join(dir, replaced)
		}
		return librarySource{Version: version, Dir: replaced}, nil
	}
	var stderr bytes.Buffer
	out, err := goOutputIn(ctx, dir, &stderr, "mod", "download", "-json", gorbitalImportPath+"@"+version)
	if err != nil {
		return librarySource{}, fmt.Errorf("download %s@%s: %w: %s", gorbitalImportPath, version, err, strings.TrimSpace(stderr.String()))
	}
	var mod struct{ Dir string }
	if err := json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		return librarySource{}, fmt.Errorf("go mod download found no source of %s@%s", gorbitalImportPath, version)
	}
	return librarySource{Version: version, Dir: mod.Dir}, nil
}
