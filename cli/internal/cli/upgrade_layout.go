package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"gorbital.dev/cli/internal/recipes"
)

const upgradeLayoutUsage = `Usage: orb upgrade --layout v0.2 [flags]

Moves a v0.1 app (a composition root in internal/app) to the v0.2 layout:
cmd/api/main.go on gorbital.Main, the app's own modules in internal/modules
and the built-in ones from the library (ADR-0083).

  - generated code you never changed is deleted: the library runs it;
  - a built-in module you changed is kept as the app's own code, copied
    from the library like orb eject, with your changes moved into the copy;
  - your modules stay in internal/modules: their huma.Register calls become
    gorbital routes, and their error mappings and permissions move from
    internal/app into each module's Module value;
  - the app's migrations, database and API stay as they are.

It writes UPGRADE-v0.2.md with what it did, what it kept and what is left
for you. The app must be on this orb's v0.1 templates: run orb upgrade
first. Nothing is committed, so git diff shows every change and git restore
undoes it.

Exit codes: 0 when the app is converted (or would be, with --dry-run), 1
when orb upgrade refuses or fails, 2 for invalid usage.
`

// layoutPreservedDir keeps the v0.1 files the conversion couldn't carry
// over. The go command ignores directories starting with an underscore, so
// the app builds with it in place.
const layoutPreservedDir = "_upgrade-v0.1"

// upgradeReportPath is the report the conversion writes into the app.
const upgradeReportPath = "UPGRADE-v0.2.md"

// compositionRoot are the directories of the v0.1 layout the library
// replaces.
var compositionRoot = []string{"internal/app/", "internal/jobs/", "cmd/migrate/", "cmd/seed/"}

// Kinds of layoutItem, which the report and the docs explain.
const (
	itemLibrary   = "library"   // generated code deleted; the library runs it
	itemTemplate  = "template"  // written from the v0.2 templates
	itemConverted = "converted" // the app's code, converted
	itemKept      = "kept"      // a built-in module kept as the app's code
	itemCarried   = "carried"   // a change moved to where it belongs in v0.2
	itemManual    = "manual"    // left for the developer; blocks the move
	itemFollowUp  = "follow-up" // left for the developer; doesn't block it
)

// layoutItem is one line of the conversion's report.
type layoutItem struct {
	Kind    string   `json:"kind"`
	Subject string   `json:"subject"`
	Detail  string   `json:"detail"`
	Notes   []string `json:"notes,omitempty"`
	Hooks   []string `json:"hooks,omitempty"`
}

// layoutChange is one file the conversion writes or deletes.
type layoutChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`

	content []byte
	before  []byte
}

type layoutResult struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	To     string `json:"to"`
	Orb    string `json:"orb"`
	DryRun bool   `json:"dry_run"`
	// Items are the report's lines, in the order they are printed.
	Items []layoutItem `json:"items"`
	// Changes are the files written, deleted or merged.
	Changes []layoutChange `json:"changes"`
	// Modules are the app's own modules converted, Ejected the built-in
	// ones kept as the app's code.
	Modules []moduleConversion `json:"modules"`
	Ejected []builtinKeep      `json:"ejected"`
	// Middleware is the app's own middleware, carried into main.go.
	Middleware *stackCarry `json:"middleware,omitempty"`
	// Conflicts are files merged with conflict markers.
	Conflicts []string `json:"conflicts"`
	// Manual are the changes the conversion couldn't make; with any of
	// them it writes nothing unless --allow-manual.
	Manual []string `json:"manual"`
	// Blocked says the conversion stopped because of Manual.
	Blocked bool `json:"blocked"`
	// Applied steps.
	Tidied   bool `json:"tidied"`
	Built    bool `json:"built"`
	Exported bool `json:"api_exported"`
	Surface  bool `json:"surface_recorded"`
	Written  bool `json:"written"`
	// Next lists the commands to run after the move.
	Next []string `json:"next"`
}

// layoutPlan is the move as planned: the files to write and delete.
type layoutPlan struct {
	res     layoutResult
	writes  map[string][]byte
	deletes []string
	goMod   []byte
	ejected []lockEjected
	appDir  string
}

func runUpgradeLayout(ctx context.Context, layout string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := newFlagSet("orb upgrade --layout", stderr, upgradeLayoutUsage)
	flags.String("layout", "", "the layout to move the app to (v0.2)")
	dryRun := flags.Bool("dry-run", false, "show what the move would do, without writing")
	diff := flags.Bool("diff", false, "print the changed files as a unified diff")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "convert although the git repository has uncommitted changes")
	allowManual := flags.Bool("allow-manual", false, "convert although some changes are left for you; they are listed and copied into "+layoutPreservedDir+"/")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	skipBuild := flags.Bool("skip-build", false, "don't build the app or record api/surface.json")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "convert without asking for confirmation")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError("unexpected arguments: " + strings.Join(flags.Args(), " "))
	}
	if layout != recipes.LayoutV02 {
		return usageError(fmt.Sprintf("orb upgrade --layout takes %s; %q is not a layout apps move to", recipes.LayoutV02, layout))
	}

	app, err := findApp()
	if err != nil {
		return err
	}
	if err := layoutPrecheck(app); err != nil {
		return err
	}
	if !insideGitRepo(ctx, app.dir) {
		return errors.New("orb upgrade --layout v0.2 changes many files at once; put the app in git first: git init && git add -A && git commit -m 'Create app'")
	}
	if !*dryRun && !*allowDirty {
		if err := requireCleanGit(ctx, app.dir); err != nil {
			return err
		}
	}
	plan, err := planLayoutMove(ctx, app, time.Now())
	if err != nil {
		return err
	}
	plan.res.DryRun = *dryRun
	plan.res.Blocked = len(plan.res.Manual) > 0 && !*allowManual

	switch {
	case plan.res.Blocked:
		if err := reportLayout(stdout, *asJSON, *diff, plan); err != nil {
			return err
		}
		return errors.New("the move is not complete without the changes listed above; make them by hand, or run orb upgrade --layout v0.2 --allow-manual to convert the rest and keep them in " + layoutPreservedDir + "/")
	case *dryRun:
		return reportLayout(stdout, *asJSON, *diff, plan)
	}
	if ask := shouldPrompt(p, *asJSON, stdin, stdout); ask && !p.yes {
		ok, err := confirm(fmt.Sprintf("Move %s to the %s layout?", plan.res.Name, recipes.LayoutV02), layoutSummary(plan), p, stdin, stderr)
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
	}
	if err := applyLayoutMove(ctx, plan, *skipTidy, *skipBuild, stderr); err != nil {
		return err
	}
	return reportLayout(stdout, *asJSON, *diff, plan)
}

// layoutPrecheck refuses an app that can't move to the v0.2 layout, before
// anything reads its files: the checks that only need gorbital.lock.
func layoutPrecheck(app appInfo) error {
	_, _, err := layoutInputs(app)
	return err
}

// layoutInputs returns the app's lock and the template inputs of the moved
// app, refusing an app the move doesn't apply to.
func layoutInputs(app appInfo) (lockFile, lockInputs, error) {
	lock, err := readLock(app.dir)
	switch {
	case errors.Is(err, errNoLock):
		return lock, lockInputs{}, fmt.Errorf("%s has no %s: orb upgrade works in apps created with orb new", app.dir, lockPath)
	case err != nil:
		return lock, lockInputs{}, err
	case lock.APIVersion != LockAPIVersion || !lock.rendered():
		return lock, lockInputs{}, errors.New(lockPath + " wasn't written by orb new, or comes from an early development build: run orb upgrade --from <commit that created the app> first")
	}
	in := lock.Inputs
	if in.layout() == recipes.LayoutV02 {
		return lock, in, errors.New("this app is already on the v0.2 layout (gorbital.Main)")
	}
	preset, ok := recipes.LookupPreset(in.Preset, in.Tenancy)
	switch {
	case !ok:
		return lock, in, fmt.Errorf("%s records an unknown preset %q", lockPath, in.Preset)
	case preset.Layout() != recipes.LayoutV02:
		return lock, in, fmt.Errorf("the %s preset has one layout, and keeps it: only Full apps move to %s (roadmap decision D17)", in.Preset, recipes.LayoutV02)
	}
	next := in
	next.Layout = recipes.LayoutV02
	return lock, next, nil
}

// planLayoutMove plans the move of a v0.1 app to the v0.2 layout.
func planLayoutMove(ctx context.Context, app appInfo, now time.Time) (*layoutPlan, error) {
	lock, next, err := layoutInputs(app)
	if err != nil {
		return nil, err
	}
	in := lock.Inputs
	d := recipes.Data{Name: in.Name, Module: in.Module, LibraryVersion: recipes.LibraryVersion}
	base, err := inputsTree(recipes.Embedded(), in, in.Mail, d)
	if err != nil {
		return nil, err
	}
	if stale := staleTemplates(lock, base); len(stale) > 0 {
		return nil, fmt.Errorf("this app isn't on this orb's v0.1 templates (%d files differ, such as %s): run orb upgrade, commit it, then orb upgrade --layout %s",
			len(stale), strings.Join(firstFew(stale, 3), ", "), recipes.LayoutV02)
	}
	target, err := inputsTree(recipes.Embedded(), next, next.Mail, d)
	if err != nil {
		return nil, err
	}
	ours, err := appFiles(ctx, app.dir)
	if err != nil {
		return nil, err
	}

	m := &layoutMove{
		app: app, lock: lock, inputs: next, base: base, target: target, ours: ours, now: now,
		writes: map[string][]byte{}, keepPaths: map[string]bool{}, handled: map[string]bool{},
		res: layoutResult{
			Name: in.Name, From: recipes.LayoutV01, To: recipes.LayoutV02, Orb: Version,
			Items: []layoutItem{}, Changes: []layoutChange{}, Modules: []moduleConversion{}, Ejected: []builtinKeep{},
			Conflicts: []string{}, Manual: []string{},
		},
	}
	if err := m.plan(ctx); err != nil {
		return nil, err
	}
	return m.finish()
}

// layoutMove carries the state of one conversion.
type layoutMove struct {
	app                appInfo
	lock               lockFile
	inputs             lockInputs
	base, target, ours map[string][]byte
	now                time.Time
	writes             map[string][]byte
	deletes            []string
	preserved          []string
	keepPaths          map[string]bool // target paths to write
	handled            map[string]bool // app or template paths already decided
	ejected            []lockEjected
	res                layoutResult
}

// plan decides what happens to every file of the app.
func (m *layoutMove) plan(ctx context.Context) error {
	if err := m.planBuiltinModules(ctx); err != nil {
		return err
	}
	m.planAppModules()
	m.planCompositionRoot()
	m.planMigrations()
	m.planGoFiles()
	return m.planOtherFiles(ctx)
}

// staleTemplates lists the files whose recorded hash isn't what this orb's
// v0.1 templates render, so plain orb upgrade has work to do first.
func staleTemplates(lock lockFile, base map[string][]byte) []string {
	var stale []string
	for _, f := range lock.Files {
		content, ok := base[f.Path]
		if !ok || sha256Hex(content) != f.SHA256 {
			stale = append(stale, f.Path)
		}
	}
	for p := range base {
		if !slices.Contains(untrackedPaths, p) && !lock.tracks(p) {
			stale = append(stale, p)
		}
	}
	sort.Strings(stale)
	return slices.Compact(stale)
}

func firstFew(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(items[:n:n], fmt.Sprintf("%d more", len(items)-n))
}

// appFiles reads the app's files that git tracks or doesn't ignore, by
// slash-separated path.
func appFiles(ctx context.Context, dir string) (map[string][]byte, error) {
	out, err := gitOutput(ctx, dir, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list the app's files: %w", err)
	}
	files := map[string][]byte{}
	for _, p := range strings.Split(out, "\n") {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, layoutPreservedDir+"/") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed but still in the index
		} else if err != nil {
			return nil, err
		}
		files[p] = content
	}
	return files, nil
}

// changedFiles returns the paths under prefix the app changed, deleted or
// added compared with the templates.
func (m *layoutMove) changedFiles(prefix string) (changed, deleted, added []string) {
	for _, p := range slices.Sorted(maps.Keys(m.base)) {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		current, ok := m.ours[p]
		switch {
		case !ok:
			deleted = append(deleted, p)
		case !bytes.Equal(current, m.base[p]):
			changed = append(changed, p)
		}
	}
	for _, p := range slices.Sorted(maps.Keys(m.ours)) {
		if strings.HasPrefix(p, prefix) {
			if _, ok := m.base[p]; !ok {
				added = append(added, p)
			}
		}
	}
	return changed, deleted, added
}

// manual records a change the conversion can't make, with the files it
// keeps in _upgrade-v0.1/ for the developer.
func (m *layoutMove) manual(subject, detail string, notes []string, keep ...string) {
	m.res.Manual = append(m.res.Manual, subject+": "+detail)
	m.res.Items = append(m.res.Items, layoutItem{Kind: itemManual, Subject: subject, Detail: detail, Notes: notes})
	m.preserve(keep...)
}

func (m *layoutMove) followUp(subject, detail string, keep ...string) {
	m.res.Items = append(m.res.Items, layoutItem{Kind: itemFollowUp, Subject: subject, Detail: detail})
	m.preserve(keep...)
}

func (m *layoutMove) item(kind, subject, detail string, notes, hooks []string) {
	m.res.Items = append(m.res.Items, layoutItem{Kind: kind, Subject: subject, Detail: detail, Notes: notes, Hooks: hooks})
}

// preserve keeps the app's v0.1 files under _upgrade-v0.1/, so nothing the
// conversion leaves behind is lost.
func (m *layoutMove) preserve(paths ...string) {
	for _, p := range paths {
		content, ok := m.ours[p]
		if !ok {
			continue
		}
		m.writes[layoutPreservedDir+"/"+p] = content
		m.preserved = append(m.preserved, p)
	}
}

// delete removes a file of the app, if it has it.
func (m *layoutMove) delete(paths ...string) {
	for _, p := range paths {
		if _, ok := m.ours[p]; ok {
			m.deletes = append(m.deletes, p)
		}
		m.handled[p] = true
	}
}

// write plans a file's content, from the templates or the conversion.
func (m *layoutMove) write(p string, content []byte) {
	m.writes[p] = content
	m.handled[p] = true
}
