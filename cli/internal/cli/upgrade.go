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

	"gorbital.dev/cli/internal/merge"
	"gorbital.dev/cli/internal/recipes"
)

// upgradeBranchPrefix starts the branch orb upgrade works on.
const upgradeBranchPrefix = "orb-upgrade/"

// derivedPaths are generated from the app's code, so upgrades regenerate
// them instead of merging (ADR-0021). api/surface.json is recorded from the
// merged code too: the upgrade commit shows what changed in it (ADR-0054).
var derivedPaths = []string{"api/openapi.json", "api/postman_collection.json", "api/llms.txt", surfacePath}

// surfacePath is the app's recorded public surface, written by its
// TestPublicSurface test (ADR-0054).
const surfacePath = "api/surface.json"

// surfaceTest is the test that records surfacePath.
const surfaceTest = "internal/app/surface_test.go"

// errConflicts reports an upgrade that left conflicts to resolve.
var errConflicts = errors.New("the upgrade has conflicts to resolve; see the files listed above")

type upgradeResult struct {
	Name      string         `json:"name"`
	From      string         `json:"from"`
	To        string         `json:"to"`
	UpToDate  bool           `json:"up_to_date"`
	Branch    string         `json:"branch,omitempty"`
	Changes   []merge.Change `json:"changes"`
	Conflicts []string       `json:"conflicts"`
	// Unproven counts files whose earlier content couldn't be proven
	// against gorbital.lock, so they were compared 2-way.
	Unproven  int  `json:"unproven"`
	Committed bool `json:"committed"`
	DryRun    bool `json:"dry_run"`
	// UserScoped lists modules the developer generated that orb add orgs
	// leaves owned by users.
	UserScoped []string `json:"user_scoped_modules,omitempty"`

	title   string // first line of the report
	message string // commit message
}

const upgradeUsage = `Usage: orb upgrade [flags]

Merges this orb's templates into the app on branch orb-upgrade/<version>.
Files you never edited take the new templates, separate edits merge, and
overlapping edits become conflict markers; your changes are never dropped.
It rebuilds what orb wrote before from the release recorded in gorbital.lock
(ADR-0050).
`

func runUpgrade(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb upgrade", flag.ContinueOnError)
	flags.SetOutput(stderr)
	from := flags.String("from", "", "release or commit that created or last upgraded the app, such as v0.4.0 (needed for apps created before v0.5 and by development builds)")
	local := flags.String("local", "", "gorbital checkout to read earlier releases from (default: the app's replace directive, or the checkout you are in)")
	dryRun := flags.Bool("dry-run", false, "show what would change, without writing or creating a branch")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	skipBuild := flags.Bool("skip-build", false, "don't build, regenerate api/openapi.json, record api/surface.json or commit")
	flags.Usage = func() {
		fmt.Fprint(stderr, upgradeUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}

	app, err := findApp()
	if err != nil {
		return err
	}
	lock, err := readLock(app.dir)
	if errors.Is(err, errNoLock) {
		return fmt.Errorf("%s has no %s: orb upgrade works in apps created with orb new", app.dir, lockPath)
	} else if err != nil {
		return err
	}
	if !insideGitRepo(ctx, app.dir) {
		return errors.New("orb upgrade works on a git branch; put the app in git first: git init && git add -A && git commit -m 'Create app'")
	}
	if !*dryRun {
		if err := requireCleanGit(ctx, app.dir); err != nil {
			return err
		}
	}

	inputs, ref, err := upgradeSource(app.dir, lock, *from)
	if err != nil {
		return err
	}
	checkout, err := releaseCheckout(ctx, app.dir, *local)
	if err != nil {
		return err
	}
	if checkout == "" && !strings.HasPrefix(ref, "v") {
		return usageError(fmt.Sprintf("reading release %s needs an gorbital checkout: pass --local <path>", ref))
	}
	old, cleanup, err := openRelease(ctx, checkout, ref, ref)
	if err != nil {
		return err
	}
	defer cleanup()

	d := recipes.Data{Name: inputs.Name, Module: inputs.Module, LibraryVersion: recipes.LibraryVersion}
	base, unproven, err := rebuildBase(old, lock, inputs, d, ref)
	if err != nil {
		return err
	}
	theirs, err := inputsTree(recipes.Embedded(), inputs, inputs.Mail, d)
	if err != nil {
		return err
	}
	next, err := lockFromTree(inputs, theirs).encode()
	if err != nil {
		return err
	}
	theirsGoMod := theirs["go.mod"]
	for _, p := range slices.Concat(untrackedPaths, derivedPaths) {
		delete(base, p)
		delete(theirs, p)
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	changes, err := merge.Plan(ctx, merge.Input{Base: base, Theirs: theirs, Unproven: unproven, Ours: rootReader(root), Label: "gorbital " + Version})
	if err != nil {
		return err
	}

	res := upgradeResult{
		Name: filepath.Base(app.dir), From: ref, To: Version, DryRun: *dryRun, Unproven: len(unproven),
		title:   fmt.Sprintf("upgrade %s from %s to gorbital %s", filepath.Base(app.dir), ref, Version),
		message: "Upgrade gorbital to " + Version,
	}
	res.setChanges(changes)
	current, _ := root.ReadFile(lockPath)
	res.UpToDate = !res.changesFiles() && bytes.Equal(current, next)
	if res.UpToDate || *dryRun {
		return reportUpgrade(stdout, *asJSON, res)
	}
	res.Branch = upgradeBranchPrefix + Version
	return applyMove(ctx, app.dir, root, &res, next, theirsGoMod, *skipTidy, *skipBuild, stdout, stderr, *asJSON)
}

// setChanges keeps the changes worth reporting and lists the conflicts.
func (r *upgradeResult) setChanges(changes []merge.Change) {
	r.Changes, r.Conflicts = []merge.Change{}, []string{}
	for _, c := range changes {
		if c.Action == merge.Unchanged && c.Note == "" {
			continue
		}
		r.Changes = append(r.Changes, c)
		if c.Action == merge.Conflict {
			r.Conflicts = append(r.Conflicts, c.Path)
		}
	}
}

func (r upgradeResult) changesFiles() bool {
	return slices.ContainsFunc(r.Changes, func(c merge.Change) bool { return c.Action != merge.Unchanged && c.Action != merge.Kept })
}

// applyMove carries out a planned move between template trees on branch
// res.Branch: it writes the changes and the new lock, updates go.mod, and
// without conflicts builds, regenerates api/openapi.json and commits. It
// reports the result, and returns errConflicts when conflicts are left.
func applyMove(ctx context.Context, dir string, root *os.Root, res *upgradeResult, lock, theirsGoMod []byte, skipTidy, skipBuild bool, stdout, stderr io.Writer, asJSON bool) error {
	var gitErr bytes.Buffer
	if err := runIn(ctx, dir, &gitErr, "git", "switch", "--quiet", "-c", res.Branch); err != nil {
		return fmt.Errorf("create branch %s: %w: %s (if an earlier run left it, finish or delete it first)", res.Branch, err, strings.TrimSpace(gitErr.String()))
	}
	if err := applyChanges(root, res.Changes); err != nil {
		return err
	}
	if err := root.WriteFile(lockPath, lock, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", lockPath, err)
	}
	if err := upgradeGoMod(ctx, dir, theirsGoMod, skipTidy, stderr); err != nil {
		return err
	}
	if len(res.Conflicts) == 0 && !skipBuild {
		if err := finishUpgrade(ctx, dir, root, res.message); err != nil {
			return fmt.Errorf("files are changed on branch %s, but: %w", res.Branch, err)
		}
		res.Committed = true
	}
	if err := reportUpgrade(stdout, asJSON, *res); err != nil {
		return err
	}
	if len(res.Conflicts) > 0 {
		return errConflicts
	}
	return nil
}

// upgradeSource returns the app's template inputs and the release that
// wrote its tracked files.
func upgradeSource(dir string, lock lockFile, from string) (lockInputs, string, error) {
	if lock.APIVersion == lockAPIVersionV1 {
		if from == "" {
			return lockInputs{}, "", usageError("gorbital.lock was written before v0.5 and doesn't record the release that created the app; pass it with --from, such as --from v0.4.0")
		}
		inputs, err := readManifest(dir)
		return inputs, from, err
	}
	switch {
	case from != "":
		return lock.Inputs, from, nil
	case lock.Orb.Revision != "":
		return lock.Inputs, lock.Orb.Revision, nil
	case lock.Orb.Version == "" || strings.Contains(lock.Orb.Version, "-dev"):
		return lockInputs{}, "", usageError(fmt.Sprintf("gorbital.lock was written by a development build of orb (%s) that recorded no commit; pass the release or commit with --from", cmpOr(lock.Orb.Version, "unknown version")))
	default:
		return lock.Inputs, lock.Orb.Version, nil
	}
}

func cmpOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// readManifest reads the template inputs from gorbital.yaml, for apps whose
// lock doesn't record them.
func readManifest(dir string) (lockInputs, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestPath))
	if err != nil {
		return lockInputs{}, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	in := lockInputs{Tenancy: recipes.TenancySingle}
	for line := range strings.Lines(string(data)) {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			in.Name = value
		case "module":
			in.Module = value
		case "preset":
			in.Preset = value
		case "tenancy":
			in.Tenancy = value
		case "mail":
			in.Mail = value
		case recipes.RowLevelSecurityKey:
			in.RLS = value == "true"
		}
	}
	if in.Name == "" || in.Module == "" || in.Preset == "" {
		return lockInputs{}, fmt.Errorf("%s needs name, module and preset", manifestPath)
	}
	if err := in.validate(manifestPath); err != nil {
		return lockInputs{}, err
	}
	return in, nil
}

// releaseCheckout returns the gorbital checkout to read releases from: the
// --local path, the checkout the app's go.mod replaces the library with, or
// the checkout the command runs in; "" when there is none. A checkout inside
// the app's own git repository is refused for --local and passed over when
// detected: go.mod and the files in that repository are the app's content,
// so a commit to the app could otherwise supply the earlier templates and
// the lock hashes that prove them, and make the upgrade revert or delete
// files (ADR-0050).
func releaseCheckout(ctx context.Context, appDir, local string) (string, error) {
	if local != "" {
		dir, err := resolveLocal(local)
		if err != nil {
			return "", err
		}
		if err := checkoutOutsideApp(ctx, appDir, dir); err != nil {
			return "", usageError(fmt.Sprintf("--local %s: %v", local, err))
		}
		return dir, nil
	}
	var candidates []string
	if info, err := readGoMod(ctx, appDir); err == nil {
		if _, dir := info.gorbital(); dir != "" {
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(appDir, dir)
			}
			candidates = append(candidates, dir)
		}
	}
	candidates = append(candidates, findCheckout())
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if p, err := resolveLocal(dir); err == nil && checkoutOutsideApp(ctx, appDir, p) == nil {
			return p, nil
		}
	}
	return "", nil
}

// checkoutOutsideApp returns an error when checkout is inside the git work
// tree appDir belongs to.
func checkoutOutsideApp(ctx context.Context, appDir, checkout string) error {
	appTop, err := gitOutput(ctx, appDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil // not in git: orb upgrade refuses such apps before this
	}
	appTop, err = filepath.EvalSymlinks(appTop)
	if err != nil {
		return err
	}
	dir, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(appTop, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("the gorbital checkout %s is inside the app's git repository %s; use a checkout of its own outside the app", checkout, appTop)
	}
	return nil
}

// rebuildBase renders what release wrote into the app and marks the files
// whose content doesn't match their hash in the lock as unproven. A v1 lock
// must match completely, since --from names its release by hand; a v2 lock
// must match at least one file.
func rebuildBase(release recipes.Release, lock lockFile, in lockInputs, d recipes.Data, ref string) (map[string][]byte, map[string]bool, error) {
	// v1 hashed files as orb new wrote them, before orb add mail recorded
	// anything; the golden apps send with Resend.
	proveMail := in.Mail
	if lock.APIVersion == lockAPIVersionV1 && in.Mail != "" {
		proveMail = recipes.MailResend
	}
	proof, err := inputsTree(release, in, proveMail, d)
	if err != nil {
		return nil, nil, fmt.Errorf("rebuild the app as %s wrote it: %w", ref, err)
	}
	unproven := map[string]bool{}
	var first string
	for _, f := range lock.Files {
		if content, ok := proof[f.Path]; !ok || sha256Hex(content) != f.SHA256 {
			unproven[f.Path] = true
			first = cmpOr(first, f.Path)
		}
	}
	switch {
	case lock.APIVersion == lockAPIVersionV1 && len(unproven) > 0:
		return nil, nil, fmt.Errorf("%d of %d files rebuilt at %s don't match gorbital.lock (such as %s): --from must name the release that created the app", len(unproven), len(lock.Files), ref, first)
	case len(lock.Files) > 0 && len(unproven) == len(lock.Files):
		return nil, nil, fmt.Errorf("no file rebuilt at %s matches gorbital.lock: that release didn't write this app; pass the right one with --from", ref)
	}
	if proveMail == in.Mail {
		return proof, unproven, nil
	}
	base, err := inputsTree(release, in, in.Mail, d)
	return base, unproven, err
}

// inputsTree renders what orb writes into an app with inputs in, sending
// email with mail, at release: the preset's tree, and row-level security in
// gorbital.yaml when orb add rls recorded it (ADR-0061).
func inputsTree(release recipes.Release, in lockInputs, mail string, d recipes.Data) (map[string][]byte, error) {
	tree, err := release.Tree(in.Preset, in.Tenancy, mail, d)
	if err == nil && in.RLS {
		recipes.SetRowLevelSecurity(tree)
	}
	return tree, err
}

// lockFromTree records tree as this orb rendered it for in.
func lockFromTree(in lockInputs, tree map[string][]byte) lockFile {
	l := lockFile{APIVersion: LockAPIVersion, Orb: lockOrb{Version: Version, Revision: buildRevision()}, Inputs: in}
	for p, content := range tree {
		if !slices.Contains(untrackedPaths, p) {
			l.Files = append(l.Files, lockedFile{Path: p, SHA256: sha256Hex(content)})
		}
	}
	slices.SortFunc(l.Files, compareLocked)
	return l
}

func rootReader(root *os.Root) func(string) ([]byte, bool, error) {
	return func(p string) ([]byte, bool, error) {
		data, err := root.ReadFile(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, false, nil
		case err != nil:
			return nil, false, err
		}
		return data, true, nil
	}
}

func applyChanges(root *os.Root, changes []merge.Change) error {
	for _, c := range changes {
		switch {
		case c.Writes():
			if dir := path.Dir(c.Path); dir != "." {
				if err := root.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create %s: %w", dir, err)
				}
			}
			if err := root.WriteFile(c.Path, c.Content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", c.Path, err)
			}
		case c.Action == merge.Delete:
			if err := root.Remove(c.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", c.Path, err)
			}
		}
	}
	return nil
}

// upgradeGoMod adds the requirements the new templates' go.mod has and the
// app's doesn't, moves gorbital modules to the new library version (unless
// the app uses a local checkout), then runs go mod tidy.
func upgradeGoMod(ctx context.Context, dir string, theirsGoMod []byte, skipTidy bool, stderr io.Writer) error {
	tmp, err := os.MkdirTemp("", "orb-gomod-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), theirsGoMod, 0o600); err != nil {
		return err
	}
	want, err := readGoMod(ctx, tmp)
	if err != nil {
		return fmt.Errorf("new templates' go.mod: %w", err)
	}
	have, err := readGoMod(ctx, dir)
	if err != nil {
		return err
	}
	_, local := have.gorbital()

	args := []string{"mod", "edit"}
	for _, req := range want.Require {
		library := req.Path == "gorbital.dev" || strings.HasPrefix(req.Path, "gorbital.dev/")
		i := slices.IndexFunc(have.Require, func(r goModRequire) bool { return r.Path == req.Path })
		switch {
		case i < 0:
			args = append(args, "-require="+req.Path+"@"+req.Version)
			if library && local != "" {
				args = append(args, "-replace="+req.Path+"="+path.Join(filepath.ToSlash(local), strings.TrimPrefix(strings.TrimPrefix(req.Path, "gorbital.dev"), "/")))
			}
		case library && local == "" && have.Require[i].Version != req.Version:
			args = append(args, "-require="+req.Path+"@"+req.Version)
		}
	}
	if len(args) > 2 {
		if err := runIn(ctx, dir, stderr, "go", args...); err != nil {
			return fmt.Errorf("update go.mod: %w", err)
		}
	}
	if !skipTidy {
		if err := runIn(ctx, dir, stderr, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("go mod tidy: %w", err)
		}
	}
	return nil
}

// finishUpgrade builds the app, regenerates api/openapi.json, records
// api/surface.json and commits with message.
func finishUpgrade(ctx context.Context, dir string, root *os.Root, message string) error {
	var out bytes.Buffer
	if err := runIn(ctx, dir, &out, "go", "build", "./..."); err != nil {
		return fmt.Errorf("the app doesn't build; fix it, regenerate api/openapi.json and commit:\n%s", out.String())
	}
	if _, err := root.Stat(derivedPaths[0]); err == nil {
		if _, err := root.Stat("cmd/api"); err == nil {
			// The API files: openapi.json, the Postman collection and llms.txt.
			var errOut bytes.Buffer
			cmd := exec.CommandContext(ctx, "go", "run", "./cmd/api", "openapi", "--dir", "api")
			cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &errOut, &errOut
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("regenerate the API files in api/: %w\n%s", err, errOut.String())
			}
		}
	}
	if _, err := root.Stat(surfaceTest); err == nil {
		var errOut bytes.Buffer
		cmd := exec.CommandContext(ctx, "go", "test", "./internal/app", "-run", "^TestPublicSurface$", "-count=1", "-update")
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &errOut, &errOut
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("record %s: %w\n%s", surfacePath, err, errOut.String())
		}
	}
	out.Reset()
	if err := runIn(ctx, dir, &out, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out.String())
	}
	if err := runIn(ctx, dir, &out, "git", "commit", "--quiet", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out.String())
	}
	return nil
}

func reportUpgrade(w io.Writer, asJSON bool, res upgradeResult) error {
	if asJSON {
		return writeJSON(w, res)
	}
	s := newStyles(w)
	if res.UpToDate {
		fmt.Fprintf(w, "%s %s is up to date with gorbital %s\n", s.muted.Render("✓"), res.Name, res.To)
		return nil
	}
	title := res.title
	if res.DryRun {
		title += " (dry run)"
	}
	fmt.Fprintf(w, "%s\n\n", s.strong.Render(title))
	for _, c := range res.Changes {
		line := fmt.Sprintf("  %-9s %s", c.Action, c.Path)
		if c.Note != "" {
			line += s.dim.Render("  " + c.Note)
		}
		if c.Action == merge.Conflict {
			line = s.accent.Render(line)
		}
		fmt.Fprintln(w, line)
	}
	if len(res.Changes) == 0 {
		fmt.Fprintln(w, "  no file changes; gorbital.lock records the new release")
	}
	if res.Unproven > 0 {
		fmt.Fprintf(w, "\n%s\n", s.dim.Render(fmt.Sprintf("%d files couldn't be proven against gorbital.lock and were compared as yours versus the release", res.Unproven)))
	}
	if len(res.UserScoped) > 0 {
		fmt.Fprintf(w, "\n  modules you generated stay owned by users: %s\n  %s\n", strings.Join(res.UserScoped, ", "),
			s.dim.Render("they keep working; to move one to organisations, generate it again with orb gen resource --scope org and move its data"))
	}

	fmt.Fprintln(w)
	switch {
	case res.DryRun:
		fmt.Fprintf(w, "  %s nothing written; run it without --dry-run to apply on a branch\n", s.dim.Render("next:"))
	case len(res.Conflicts) > 0:
		fmt.Fprintf(w, "  on branch %s, not committed. Resolve the markers (<<<<<<< yours … >>>>>>> gorbital %s) in:\n", res.Branch, res.To)
		for _, p := range res.Conflicts {
			fmt.Fprintf(w, "    %s\n", p)
		}
		fmt.Fprintf(w, "\n  %s go build ./...\n        go run ./cmd/api openapi --dir api\n        go test ./internal/app -run TestPublicSurface -update\n        go test ./...\n        git add -A && git commit -m '%s'\n", s.dim.Render("next:"), res.message)
	case res.Committed:
		fmt.Fprintf(w, "  committed on branch %s\n\n  %s go test ./...   (database tests need orb dev or docker compose up -d --wait)\n        then merge %s\n", res.Branch, s.dim.Render("next:"), res.Branch)
	default:
		fmt.Fprintf(w, "  on branch %s, not committed\n\n  %s go build ./...\n        go run ./cmd/api openapi --dir api\n        go test ./internal/app -run TestPublicSurface -update\n        git add -A && git commit -m '%s'\n", res.Branch, s.dim.Render("next:"), res.message)
	}
	return nil
}
