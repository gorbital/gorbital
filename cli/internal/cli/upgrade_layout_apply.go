package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

// newFlagSet returns a flag set that prints usage before its flags.
func newFlagSet(name string, stderr io.Writer, usage string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, usage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	return flags
}

// applyLayoutMove writes the move: the files, the module list, go.mod,
// gofmt, the build and api/surface.json.
func applyLayoutMove(ctx context.Context, plan *layoutPlan, skipTidy, skipBuild bool, stderr io.Writer) error {
	root, err := os.OpenRoot(plan.appDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, p := range slices.Sorted(mapsKeys(plan.writes)) {
		if dir := path.Dir(p); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
		}
		if err := root.WriteFile(p, plan.writes[p], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	for _, p := range plan.deletes {
		if _, written := plan.writes[p]; written {
			continue
		}
		if err := root.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", p, err)
		}
	}
	if err := pruneEmptyDirs(plan.appDir, plan.deletes); err != nil {
		return err
	}
	plan.res.Written = true

	app, err := findAppIn(plan.appDir)
	if err != nil {
		return err
	}
	modules, err := planModules(app)
	if err != nil {
		return fmt.Errorf("write the module list: %w", err)
	}
	if err := genplan.Apply(plan.appDir, modules); err != nil {
		return err
	}
	if err := upgradeGoMod(ctx, plan.appDir, plan.goMod, skipTidy, stderr); err != nil {
		return err
	}
	plan.res.Tidied = !skipTidy
	var out bytes.Buffer
	if err := runIn(ctx, plan.appDir, &out, "gofmt", "-w", "."); err != nil {
		return fmt.Errorf("the app is converted, but gofmt failed: %w\n%s", err, out.String())
	}
	if skipBuild {
		return nil
	}
	out.Reset()
	if err := runIn(ctx, plan.appDir, &out, "go", "build", "./..."); err != nil {
		return fmt.Errorf("the app is converted, but it doesn't build; fix what %s lists, then run go build ./... again:\n%s", upgradeReportPath, out.String())
	}
	plan.res.Built = true
	if _, err := root.Stat(derivedPaths[0]); err == nil {
		// The API files the app publishes: the document, the Postman
		// collection and llms.txt, regenerated from the converted code so
		// git diff shows what the move changed in them.
		out.Reset()
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/api", "openapi", "--dir", "api")
		cmd.Dir, cmd.Stdout, cmd.Stderr = plan.appDir, &out, &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("the app is converted and builds, but exporting the API files failed: %w\n%s", err, out.String())
		}
		plan.res.Exported = true
	}
	recorded, err := recordSurface(ctx, plan.appDir)
	if err != nil {
		return fmt.Errorf("the app is converted and builds, but %w", err)
	}
	plan.res.Surface = recorded
	return nil
}

// recordSurface records the app's public names in api/surface.json with its
// own test, as orb upgrade does after merging (ADR-0054). It reports whether
// the app has that test at all.
func recordSurface(ctx context.Context, dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash("internal/modules/surface_test.go"))); err != nil {
		return false, nil
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/modules", "-run", "^TestPublicSurface$", "-count=1", "-update")
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &out, &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("recording %s failed: %w\n%s", surfacePath, err, out.String())
	}
	return true, nil
}

// pruneEmptyDirs removes the directories the move emptied, deepest first.
func pruneEmptyDirs(dir string, deleted []string) error {
	dirs := map[string]bool{}
	for _, p := range deleted {
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	list := slices.Sorted(mapsKeys(dirs))
	slices.Reverse(list)
	for _, d := range list {
		entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(d)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(d))); err != nil {
				return err
			}
		}
	}
	return nil
}

// mapsKeys returns a map's keys as a sequence, for slices.Sorted.
func mapsKeys[K comparable, V any](m map[K]V) func(func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// layoutSummary is the short description of the move the confirmation asks
// about.
func layoutSummary(plan *layoutPlan) string {
	writes, deletes, conflicts := 0, 0, len(plan.res.Conflicts)
	for _, c := range plan.res.Changes {
		if c.Action == "delete" {
			deletes++
		} else {
			writes++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  Files:       %d written, %d deleted", writes, deletes)
	if conflicts > 0 {
		fmt.Fprintf(&b, ", %d with conflict markers", conflicts)
	}
	b.WriteString("\n")
	if len(plan.res.Modules) > 0 {
		names := make([]string, len(plan.res.Modules))
		for i, c := range plan.res.Modules {
			names[i] = c.Name
		}
		fmt.Fprintf(&b, "  Modules:     %s converted\n", strings.Join(names, ", "))
	}
	if len(plan.res.Ejected) > 0 {
		names := make([]string, len(plan.res.Ejected))
		for i, e := range plan.res.Ejected {
			names[i] = e.Module
		}
		fmt.Fprintf(&b, "  Kept:        %s, as the app's own code\n", strings.Join(names, ", "))
	}
	if len(plan.res.Manual) > 0 {
		fmt.Fprintf(&b, "  Manual:      %d changes left for you\n", len(plan.res.Manual))
	}
	fmt.Fprintf(&b, "  Report:      %s\n", upgradeReportPath)
	return b.String()
}

// reportLayout prints the move: one line per decision, then the files and
// what to do next.
func reportLayout(w io.Writer, asJSON, diff bool, plan *layoutPlan) error {
	res := plan.res
	if asJSON {
		return writeJSON(w, res)
	}
	s := newStyles(w)
	title := fmt.Sprintf("move %s from the %s layout to %s (gorbital.Main)", res.Name, res.From, res.To)
	switch {
	case res.Blocked:
		title += " (not done)"
	case res.DryRun:
		title += " (dry run)"
	}
	fmt.Fprintf(w, "%s\n\n", s.strong.Render(title))
	for _, item := range res.Items {
		line := fmt.Sprintf("  %-10s %s", item.Kind, item.Subject)
		if item.Kind == itemManual {
			line = s.accent.Render(line)
		}
		fmt.Fprintf(w, "%s  %s\n", line, s.dim.Render(item.Detail))
		for _, note := range item.Notes {
			for _, l := range strings.Split(note, "\n") {
				fmt.Fprintf(w, "             %s\n", s.dim.Render(l))
			}
		}
		if len(item.Hooks) > 0 {
			fmt.Fprintf(w, "             %s\n", s.dim.Render("could become: "+strings.Join(item.Hooks, ", ")))
		}
	}
	fmt.Fprintln(w, "\n"+layoutSummary(plan))
	if diff {
		fmt.Fprint(w, layoutDiff(plan))
	}
	switch {
	case res.Blocked:
		fmt.Fprintf(w, "  %s nothing was written. Make the changes marked %s, or convert the rest with --allow-manual\n",
			s.dim.Render("next:"), s.accent.Render("manual"))
	case res.DryRun:
		fmt.Fprintf(w, "  %s nothing written; run it without --dry-run to convert the app\n", s.dim.Render("next:"))
	default:
		fmt.Fprintf(w, "  %s\n", s.dim.Render("next:"))
		for _, step := range res.Next {
			fmt.Fprintf(w, "        %s\n", step)
		}
		fmt.Fprintf(w, "\n  %s\n", s.dim.Render("nothing is committed: git diff shows every change, git restore . undoes them, and "+upgradeReportPath+" says what was done"))
	}
	return nil
}

// layoutDiff renders the changed files as a unified diff, as orb gen --diff
// does.
func layoutDiff(plan *layoutPlan) string {
	var changes []genplan.Change
	for _, c := range plan.res.Changes {
		switch c.Action {
		case "delete":
			continue
		case "create":
			changes = append(changes, genplan.Change{Path: c.Path, Kind: genplan.Create, Content: c.content})
		default:
			changes = append(changes, genplan.Change{Path: c.Path, Kind: genplan.Modify, Before: c.before, Content: c.content})
		}
	}
	return genplan.Diff(genplan.Plan{Changes: changes})
}

// report is UPGRADE-v0.2.md: what the move did, file by file, what it kept
// and what is left for the developer.
func (m *layoutMove) report() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s on the v0.2 layout\n\n", m.inputs.Name)
	fmt.Fprintf(&b, "orb %s moved this app from the v0.1 layout (a composition root in\n"+
		"`internal/app`) to the v0.2 layout: `cmd/api/main.go` on `gorbital.Main`, the\n"+
		"app's modules in `internal/modules`, the built-in ones from\n"+
		"`gorbital.dev/gorbital` (ADR-0083). The database, the migration history and\n"+
		"the API are unchanged.\n\n", Version)
	b.WriteString("Delete this file once you have read it; `git log` keeps the move.\n\n")

	b.WriteString("## What happened\n\n")
	for _, kind := range []string{itemLibrary, itemTemplate, itemConverted, itemKept, itemCarried} {
		for _, item := range m.res.Items {
			if item.Kind != kind {
				continue
			}
			fmt.Fprintf(&b, "- **%s** `%s`: %s\n", item.Kind, item.Subject, item.Detail)
			for _, note := range item.Notes {
				fmt.Fprintf(&b, "  - %s\n", strings.ReplaceAll(note, "\n", "\n    "))
			}
			if len(item.Hooks) > 0 {
				fmt.Fprintf(&b, "  - could become, instead of code you own: %s\n", strings.Join(item.Hooks, ", "))
			}
		}
	}
	if len(m.res.Ejected) > 0 {
		b.WriteString("\n## Modules the app now owns\n\n")
		b.WriteString("They are copies of the library's modules, like `orb eject`: library releases\n" +
			"no longer change them, `orb doctor` says when the library's version changes,\n" +
			"and `gorbital.lock` records where each came from.\n\n")
		for _, e := range m.res.Ejected {
			fmt.Fprintf(&b, "- `internal/modules/%s` from `%s` %s\n", e.Module, e.Package, e.Version)
			for _, c := range e.Carried {
				fmt.Fprintf(&b, "  - %s\n", c)
			}
			for _, n := range e.Notes {
				fmt.Fprintf(&b, "  - %s\n", n)
			}
			if len(e.Hooks) > 0 {
				fmt.Fprintf(&b, "  - to go back to the library's module, these changes could become: %s\n", strings.Join(e.Hooks, ", "))
			}
		}
	}
	if len(m.res.Manual) > 0 || len(m.res.Conflicts) > 0 {
		b.WriteString("\n## Left for you\n\n")
		for _, item := range m.res.Items {
			if item.Kind != itemManual && item.Kind != itemFollowUp {
				continue
			}
			fmt.Fprintf(&b, "- **%s** `%s`: %s\n", item.Kind, item.Subject, item.Detail)
			for _, note := range item.Notes {
				fmt.Fprintf(&b, "  - %s\n", strings.ReplaceAll(note, "\n", "\n    "))
			}
		}
		for _, p := range m.res.Conflicts {
			fmt.Fprintf(&b, "- **conflict** `%s`: merged with conflict markers; resolve them\n", p)
		}
		if len(m.preserved) > 0 {
			fmt.Fprintf(&b, "\nThe v0.1 files are kept in `%s/`, which the go command ignores; delete the\n"+
				"directory once nothing there is needed.\n", layoutPreservedDir)
		}
	}

	b.WriteString("\n## The new layout\n\n```text\n")
	b.WriteString("cmd/api/main.go        gorbital.Main: the app's name, sign-in, modules, migrations\n" +
		"cmd/api/mail.go        the email provider (orb add mail)\n" +
		"cmd/api/storage.go     S3-compatible file storage\n" +
		"internal/modules/      the app's modules, one directory each, and modules.gen.go\n" +
		"db/migrations/         the app's own migrations; the library declares its own\n" +
		"api/                   openapi.json, the Postman collection, llms.txt, surface.json\n```\n\n")
	b.WriteString("What used to be `internal/app`, `internal/jobs`, `cmd/migrate` and `cmd/seed`\n" +
		"is the library's now: `go run ./cmd/api migrate`, `go run ./cmd/api seed`,\n" +
		"`go run ./cmd/api help` lists every command.\n")

	b.WriteString("\n## Next\n\n```sh\n")
	for _, step := range m.res.Next {
		fmt.Fprintf(&b, "%s\n", step)
	}
	b.WriteString("```\n\n## Going back\n\n")
	b.WriteString("Nothing is committed. `git restore . && git clean -fd` undoes the move; after\n" +
		"you commit it, `git revert <commit>` does. The v0.1 layout keeps working with\n" +
		"`gorbital.dev` v0.2 releases: plain `orb upgrade` merges its templates as before\n" +
		"(docs/start/upgrading.md).\n")
	return []byte(b.String())
}

var _ = recipes.LayoutV02
