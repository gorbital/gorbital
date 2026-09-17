package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/recipes"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*$`)
)

// maxNameLength keeps app names valid as PostgreSQL role and database names
// in the Full preset's compose.yaml.
const maxNameLength = 63

type newResult struct {
	Name    string `json:"name"`
	Module  string `json:"module"`
	Dir     string `json:"dir"`
	Preset  string `json:"preset"`
	Tenancy string `json:"tenancy"`
	Files   int    `json:"files"`
}

func runNew(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	module := flags.String("module", "", "Go module path (default: the app name)")
	preset := flags.String("preset", "minimal", "preset: minimal (HTTP API, no database) or full (PostgreSQL, authentication, jobs, email, audit, ops APIs)")
	tenancy := flags.String("tenancy", recipes.TenancySingle, "who owns the data (Full preset): single (users) or multi (organisations with members, roles and invitations)")
	local := flags.String("local", "", "path to an gorbital checkout, used through replace directives (default: the checkout you are in, if any)")
	noGit := flags.Bool("no-git", false, "don't initialise a git repository")
	start := flags.Bool("start", false, "run orb dev in the new app and open the Dev Portal when it is created (the default in a terminal; --no-start turns it off)")
	noStart := flags.Bool("no-start", false, "don't run orb dev afterwards")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: orb new [<name>] [flags]\n\nIn a terminal, missing values are asked interactively; pass flags to skip them.\n\nFlags:")
		flags.PrintDefaults()
	}

	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" && flags.NArg() > 0 {
		name = flags.Arg(0)
	} else if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	// Reject a bad --preset or --tenancy before asking anything else.
	if _, err := lookupPreset(*preset, *tenancy); err != nil {
		return err
	}

	// Apps use the published library unless --local names a checkout; inside a
	// checkout, use that one, for working on gorbital itself.
	detected := false
	if *local == "" {
		if dir := findCheckout(); dir != "" {
			*local, detected = dir, true
		}
	}

	ask := shouldPrompt(p, *asJSON, stdin, stdout)
	if ask {
		if err := promptNew(&name, module, preset, tenancy, local, noGit, set, p, stdin, stderr); err != nil {
			return err
		}
	}

	if err := validateName(name); err != nil {
		return err
	}
	if *module == "" {
		*module = name
	}
	if err := validateModule(*module); err != nil {
		return err
	}
	chosen, err := lookupPreset(*preset, *tenancy)
	if err != nil {
		return err
	}
	localPath, err := resolveLocal(*local)
	if err != nil {
		return err
	}

	if _, err := os.Lstat(name); err == nil {
		return fmt.Errorf("%s already exists; choose another name or remove it", name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if ask {
		ok, err := newAsker(p, stdin, stderr).confirm(fmt.Sprintf("create %s in ./%s?", name, name))
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
		fmt.Fprintln(stderr)
	}

	// The log goes to stdout; --json prints only the result.
	log := stdout
	if *asJSON {
		log = io.Discard
	}
	s := newStyles(stdout)
	step := func(done string) { fmt.Fprintf(log, "%s %s\n", s.muted.Render("✓"), done) }
	about := "preset " + chosen.Name
	if recipes.SupportsTenancy(chosen.Name) {
		about += " · tenancy " + chosen.Tenancy
	}
	fmt.Fprintf(log, "creating %s in ./%s\n%s\n\n", name, name, s.dim.Render(about+" · "+libraryLine(localPath, detected)))

	if err := os.Mkdir(name, 0o755); err != nil {
		return err
	}

	files, err := create(name, chosen, recipes.Data{Name: name, Module: *module, LibraryVersion: recipes.LibraryVersion, Local: localPath})
	if err != nil {
		return errors.Join(err, os.RemoveAll(name)) // we created the directory; remove the partial app
	}
	step(fmt.Sprintf("wrote %d files", len(files)))

	if !*skipTidy {
		var out bytes.Buffer
		if err := runIn(ctx, name, &out, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("created %s, but go mod tidy failed: %w\n%s"+
				"  check your network and GOPROXY, or create the app from a checkout with --local <path to gorbital checkout>", name, err, out.String())
		}
		step("ran go mod tidy")
	}
	if !*noGit {
		if _, err := exec.LookPath("git"); err == nil {
			if err := runIn(ctx, name, stderr, "git", "init", "--quiet"); err != nil {
				return fmt.Errorf("created %s, but git init failed: %w", name, err)
			}
			step("initialised git")
		} else {
			fmt.Fprintln(log, s.dim.Render("  git not found, skipped git init"))
		}
	}

	res := newResult{Name: name, Module: *module, Dir: name, Preset: chosen.Name, Tenancy: chosen.Tenancy, Files: len(files)}
	if *asJSON {
		return writeJSON(stdout, res)
	}
	fmt.Fprintf(stdout, "\n%s\n\n%s", s.strong.Render("created "+name), nextSteps(s, name, chosen))
	// The first run ends in the browser: in a terminal, orb dev starts and
	// opens the Dev Portal unless --no-start (ADR-0077).
	if *start || (!*noStart && ask) {
		fmt.Fprintf(stdout, "\n%s\n", s.dim.Render("starting orb dev in "+name+" (Ctrl-C stops it; --no-start skips this)"))
		if err := os.Chdir(name); err != nil {
			return err
		}
		return runDev(ctx, nil, stderr)
	}
	return nil
}

// lookupPreset returns the preset orb new --preset name --tenancy tenancy
// selects, or a usage error naming what exists.
func lookupPreset(name, tenancy string) (recipes.Preset, error) {
	switch {
	case name == "custom":
		return recipes.Preset{}, usageError("preset custom isn't available yet; use --preset minimal or --preset full")
	case !slices.Contains(recipes.PresetNames(), name):
		return recipes.Preset{}, usageError(fmt.Sprintf("unknown preset %q (want %s)", name, strings.Join(recipes.PresetNames(), " or ")))
	case tenancy != recipes.TenancySingle && tenancy != recipes.TenancyMulti:
		return recipes.Preset{}, usageError(fmt.Sprintf("unknown tenancy %q (want single or multi)", tenancy))
	}
	p, ok := recipes.LookupPreset(name, tenancy)
	if !ok {
		return recipes.Preset{}, usageError(fmt.Sprintf("--tenancy %s needs the Full preset: organisations need its database and authentication (use --preset full)", tenancy))
	}
	return p, nil
}

// nextSteps lists where things are in a new app of the preset in dir, and
// ends with the commands to run next.
func nextSteps(s styles, dir string, preset recipes.Preset) string {
	rows := [][2]string{
		{"api docs", "http://127.0.0.1:8080/docs"},
		{"traces", "orb dev --observability (needs Docker)"},
	}
	if preset.Name == "full" {
		rows = [][2]string{
			{"api docs", "http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)"},
			{"emails", "http://127.0.0.1:8025 (Mailpit catches every email in development)"},
			{"admin", "admin@example.com; orb dev prints its password, 2FA key and recovery codes once"},
			{"sign-in", "AUTH_PROVIDERS.md lists what to set for passkeys, Google and Apple"},
		}
		if preset.Tenancy == recipes.TenancyMulti {
			rows = append(rows,
				[2]string{"orgs", "every account gets a personal workspace; data lives under /v1/orgs/{orgId}"},
				[2]string{"invitations", "set orgs.invitation_url to your frontend's page before inviting people"},
			)
		}
		rows = append(rows, [][2]string{
			{"email", "Resend outside development; orb add mail switches to SMTP"},
			{"port 5432", "taken? set POSTGRES_PORT in .env and the same port in DATABASE_URL"},
			{"without orb", "cp .env.example .env, docker compose up -d --wait,"},
			{"", "go run ./cmd/migrate, go run ./cmd/seed, go run ./cmd/api"},
		}...)
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "  %s %s\n", s.dim.Render(fmt.Sprintf("%-12s", row[0])), row[1])
	}
	fmt.Fprintf(&b, "\n  %s cd %s\n        %s\n", s.dim.Render("next:"), dir, s.accent.Render("orb dev"))
	return b.String()
}

// promptNew asks, one question at a time, for every value not given by a
// flag. Values given by flag are shown as answered lines first.
func promptNew(name, module, preset, tenancy, local *string, noGit *bool, set map[string]bool, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	a := newAsker(p, stdin, stderr)

	if *name == "" {
		input := huh.NewInput().Title(a.title("app name")).Inline(true).Prompt("").
			Placeholder("my-api").Value(name).Validate(validateName)
		if err := a.ask(input, "app name", func() string { return *name }); err != nil {
			return err
		}
	} else {
		a.answered("app name", *name)
	}

	moduleOrName := func() string { return cmp.Or(*module, *name) }
	if !set["module"] {
		input := huh.NewInput().Title(a.title("Go module path")).Inline(true).Prompt("").
			Placeholder(*name).Value(module).
			Validate(func(s string) error {
				if s == "" {
					return nil
				}
				return validateModule(s)
			})
		if err := a.ask(input, "Go module path", moduleOrName); err != nil {
			return err
		}
	} else {
		a.answered("Go module path", moduleOrName())
	}

	if !set["preset"] {
		sel := huh.NewSelect[string]().Title(a.choiceTitle("preset")).Options(
			huh.NewOption("minimal  HTTP API, config, telemetry, health checks, docs · no database", "minimal"),
			huh.NewOption("full     PostgreSQL, auth, jobs, email, audit, ops APIs · needs Docker", "full"),
		).Value(preset)
		if err := a.ask(sel, "preset", func() string { return *preset }); err != nil {
			return err
		}
	} else {
		a.answered("preset", *preset)
	}

	// Only presets with a multi-tenant variant ask who owns the data (ADR-0023).
	switch {
	case !recipes.SupportsTenancy(*preset):
		*tenancy = recipes.TenancySingle
	case !set["tenancy"]:
		sel := huh.NewSelect[string]().Title(a.choiceTitle("tenancy")).Options(
			huh.NewOption("single  one user base: records belong to users", recipes.TenancySingle),
			huh.NewOption("multi   companies or teams: organisations with members, roles and invitations", recipes.TenancyMulti),
		).Value(tenancy)
		if err := a.ask(sel, "tenancy", func() string { return *tenancy }); err != nil {
			return err
		}
	default:
		a.answered("tenancy", *tenancy)
	}

	// The library comes from the module proxy; only --local, or running inside
	// a checkout, uses a checkout instead, so there is nothing to ask.
	if *local != "" {
		a.answered("gorbital checkout", *local)
	} else {
		a.answered("library", "gorbital.dev "+recipes.LibraryVersion)
	}

	gitInit := !*noGit
	if !set["no-git"] {
		if err := a.ask(a.yesNo("initialise a git repository?", &gitInit), "initialise a git repository?", func() string { return yesNo(gitInit) }); err != nil {
			return err
		}
	} else {
		a.answered("initialise a git repository?", yesNo(gitInit))
	}
	*noGit = !gitInit
	return nil
}

// libraryLine says which gorbital the app is built against.
func libraryLine(local string, detected bool) string {
	switch {
	case local == "":
		return "library gorbital.dev " + recipes.LibraryVersion
	case detected:
		return "library " + relPath(local) + " (found above this directory; --local to change)"
	default:
		return "library " + relPath(local)
	}
}

// relPath shows path relative to the current directory when that's shorter
// to read: at most two levels up, otherwise the absolute path.
func relPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(wd, filepath.FromSlash(path))
	if err != nil {
		return path
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../../../") {
		return path
	}
	return rel
}

// findCheckout returns the nearest directory at or above the current one
// whose go.mod declares module gorbital.dev, or "".
func findCheckout() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && bytes.HasPrefix(data, []byte("module gorbital.dev\n")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// create renders the preset into dir and records this release, the inputs
// and the tracked files' hashes in gorbital.lock (ADR-0050).
func create(dir string, preset recipes.Preset, d recipes.Data) ([]recipes.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	files, err := preset.Render(root, d)
	if err != nil {
		return nil, err
	}
	if err := writeLock(root, newLock(preset, d, files)); err != nil {
		return nil, err
	}
	return files, nil
}

func validateName(name string) error {
	switch {
	case name == "":
		return usageError("missing app name: orb new <name> (or run it in a terminal to be asked)")
	case len(name) > maxNameLength || !namePattern.MatchString(name):
		return usageError(fmt.Sprintf("invalid app name %q: use lowercase letters, digits and single hyphens, starting with a letter (max %d characters)", name, maxNameLength))
	}
	return nil
}

func validateModule(module string) error {
	for _, seg := range strings.Split(module, "/") {
		if !segmentPattern.MatchString(seg) || seg == "." || seg == ".." {
			return usageError(fmt.Sprintf("invalid module path %q", module))
		}
	}
	return nil
}

func resolveLocal(local string) (string, error) {
	if local == "" {
		return "", nil
	}
	abs, err := filepath.Abs(local)
	if err != nil {
		return "", err
	}
	goMod, err := os.ReadFile(filepath.Join(abs, "go.mod"))
	if err != nil || !strings.HasPrefix(string(goMod), "module gorbital.dev\n") {
		return "", usageError(fmt.Sprintf("--local %s is not an gorbital checkout (no go.mod with module gorbital.dev)", local))
	}
	return filepath.ToSlash(abs), nil
}

func runIn(ctx context.Context, dir string, output io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = output, output
	return cmd.Run()
}
