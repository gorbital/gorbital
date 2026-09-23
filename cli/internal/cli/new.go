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
	"time"

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/genplan"
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
	// Auth is how much sign-in the app has: none, basic or full; empty for
	// the Minimal preset, which has none of gorbital's.
	Auth string `json:"auth,omitempty"`
	// Scope is what a tenant is: none, single, custom or its name.
	Scope string `json:"scope,omitempty"`
	// Files counts the files the preset's templates wrote.
	Files int `json:"files"`
	// Ejected are the built-in modules copied into internal/modules, in the
	// order they were copied: sign-in, and organisations with --tenancy
	// multi.
	Ejected []newEjected `json:"ejected"`
}

func runNew(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	module := flags.String("module", "", "Go module path, which every import in the app starts with (default: the app name; not asked)")
	preset := flags.String("preset", "minimal", "preset: minimal (HTTP API, no database) or full (PostgreSQL, authentication, jobs, email, audit, ops APIs)")
	tenancy := flags.String("tenancy", recipes.TenancySingle, "deprecated alias of --scope: single means --scope single, multi means --scope "+recipes.DefaultScopeName)
	auth := flags.String("auth", "", "how much sign-in: "+recipes.AuthUsage+" (default: full). none has no accounts and no auth tables; basic is an email address, a password and the operators' account APIs")
	scope := flags.String("scope", "", "what a tenant is: "+recipes.ScopeNameUsage+" (default: single). A name such as "+recipes.DefaultScopeName+" or merchant mounts the supplied organisations under that word; custom writes the membership rules into the app")
	local := flags.String("local", "", "path to a gorbital checkout, used through replace directives (default: the checkout you are in, if any)")
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
	// --auth and --scope describe an app on gorbital.Main, so they choose
	// the Full preset when none was named; --preset minimal is unchanged,
	// and so is --tenancy, which the Minimal preset has always refused.
	if !set["preset"] && (set["auth"] || set["scope"]) {
		*preset = "full"
	}
	// --tenancy is the v0.2 spelling of --scope, kept for all of v0.x.
	if set["tenancy"] && !set["scope"] && *preset == "full" {
		p, err := recipes.ProfileFromTenancy(*tenancy)
		if err != nil {
			return usageError(err.Error())
		}
		*scope, set["scope"] = p.Scope, true
	}
	if *preset != "full" && (*tenancy != recipes.TenancySingle || set["tenancy"]) {
		if _, err := recipes.ProfileFromTenancy(*tenancy); err != nil {
			return usageError(err.Error())
		}
		if *tenancy != recipes.TenancySingle {
			return usageError(fmt.Sprintf("--tenancy %s needs the Full preset: organisations need its database and authentication (use --preset full)", *tenancy))
		}
	}
	// Reject a bad --preset, --auth or --scope before asking anything else.
	if _, _, err := lookupShape(*preset, *auth, *scope); err != nil {
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
		if err := promptNew(&name, module, preset, auth, scope, local, noGit, set, p, stdin, stderr); err != nil {
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
	chosen, profile, err := lookupShape(*preset, *auth, *scope)
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
	if chosen.Layout() == recipes.LayoutV02 {
		about = "auth " + profile.Auth + " · scope " + profile.Scope
	}
	fmt.Fprintf(log, "creating %s in ./%s\n%s\n\n", name, name, s.dim.Render(about+" · "+libraryLine(localPath, detected)))

	if err := os.Mkdir(name, 0o755); err != nil {
		return err
	}

	// The demonstration module is generated, not templated: orb gen module
	// writes it at the profile's access rule, which is the only thing that
	// differed between the shapes (ADR-0090 §5). It runs before the lock is
	// written, because it rewrites two of the rendered files.
	demo := func() error { return nil }
	if chosen.Layout() == recipes.LayoutV02 {
		demo = func() error { return generateDemoModule(name, *module, profile) }
	}
	files, err := create(name, chosen, profile, recipes.Data{Name: name, Module: *module, LibraryVersion: recipes.LibraryVersion, Local: localPath, Profile: profile}, demo)
	if err != nil {
		return errors.Join(err, os.RemoveAll(name)) // we created the directory; remove the partial app
	}
	step(fmt.Sprintf("wrote %d files", len(files)))
	if chosen.Layout() == recipes.LayoutV02 {
		step("generated the projects module, owned by " + demoOwner(profile))
	}

	// Sign-in and organisations are the app's code from the start (v0.2.1):
	// the modules are copied from the library version go.mod requires.
	// There is one shape of Full app, and no flag to ask for the other
	// one (ADR-0092).
	ejected := []newEjected{}
	if modules := copies(chosen, profile); len(modules) > 0 {
		lib, err := newAppLibrary(ctx, name, localPath)
		if err == nil {
			ejected, err = ejectIntoNewApp(name, modules, lib, time.Now())
		}
		if err != nil {
			return errors.Join(fmt.Errorf("copy sign-in into the app: %w\n"+
				"  check your network and GOPROXY, then try again", err), os.RemoveAll(name))
		}
		for _, e := range ejected {
			step(fmt.Sprintf("copied %s into %s: %d files and %d migrations, from %s %s", ejectedAbout(e.Module), e.Directory, e.Files, len(e.Migrations), e.Package, e.Version))
		}
	}

	if !*skipTidy {
		var out bytes.Buffer
		if err := runIn(ctx, name, &out, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("created %s, but go mod tidy failed: %w\n%s"+
				"  check your network and GOPROXY, or create the app from a checkout with --local <path to gorbital checkout>", name, err, out.String())
		}
		step("ran go mod tidy")
		// The API artefacts are the app's own output, not templates: it
		// writes them itself, exactly as CI does for the golden apps
		// (ADR-0090 §5).
		if chosen.Layout() == recipes.LayoutV02 {
			if err := produceAPI(ctx, name); err != nil {
				return fmt.Errorf("created %s, but %w", name, err)
			}
			step("wrote " + strings.Join(recipes.APIArtefacts(), ", "))
		}
		// The app's public names, the copied modules' among them, are
		// recorded as orb eject recorded them (ADR-0054).
		if chosen.Layout() == recipes.LayoutV02 {
			if _, err := recordSurface(ctx, name); err != nil {
				return fmt.Errorf("created %s, but %w", name, err)
			}
			recorded := "recorded " + surfacePath
			if len(ejected) > 0 {
				recorded += ": the copied modules' error codes and audit actions are the app's"
			}
			step(recorded)
		}
	} else if chosen.Layout() == recipes.LayoutV02 {
		fmt.Fprintln(log, s.dim.Render("  --skip-tidy: once the module is tidy, write the API artefacts with go run ./cmd/api openapi --dir api"+
			", and record the copied modules' names with "+surfaceCommand(recipes.LayoutV02)))
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

	res := newResult{Name: name, Module: *module, Dir: name, Preset: chosen.Name, Tenancy: chosen.Tenancy, Files: len(files), Ejected: ejected}
	if chosen.Layout() == recipes.LayoutV02 {
		res.Tenancy, res.Auth, res.Scope = profile.Tenancy(), profile.Auth, profile.Scope
	}
	if *asJSON {
		return writeJSON(stdout, res)
	}
	fmt.Fprintf(stdout, "\n%s\n\n%s", s.strong.Render("created "+name), nextSteps(s, name, chosen, profile, len(ejected) > 0))
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

// lookupShape returns the app orb new --preset name --auth auth --scope
// scope creates: the preset whose templates and layout it uses, and, for
// an app on gorbital.Main, the profile that decides what those templates
// write. An illegal combination is refused with a message saying why
// (ADR-0090 §2).
func lookupShape(name, auth, scope string) (recipes.Preset, recipes.Profile, error) {
	switch {
	case name == "custom":
		return recipes.Preset{}, recipes.Profile{}, usageError("preset custom isn't available yet; use --preset minimal, or --auth and --scope")
	case !slices.Contains(recipes.PresetNames(), name):
		return recipes.Preset{}, recipes.Profile{}, usageError(fmt.Sprintf("unknown preset %q (want %s)", name, strings.Join(recipes.PresetNames(), " or ")))
	}
	preset, ok := recipes.LookupPreset(name, recipes.TenancySingle)
	if !ok {
		return recipes.Preset{}, recipes.Profile{}, usageError(fmt.Sprintf("unknown preset %q", name))
	}
	if preset.Layout() != recipes.LayoutV02 {
		if auth != "" || scope != "" {
			return recipes.Preset{}, recipes.Profile{}, usageError(fmt.Sprintf("the %s preset has no sign-in and no tenants, so --auth and --scope don't apply to it", name))
		}
		return preset, recipes.Profile{}, nil
	}
	profile, err := recipes.ParseProfile(auth, scope)
	if err != nil {
		return recipes.Preset{}, recipes.Profile{}, usageError(err.Error())
	}
	return preset, profile, nil
}

// copies returns the built-in modules orb new copies into the app: the
// profile's for an app on gorbital.Main, and the preset's otherwise.
func copies(preset recipes.Preset, profile recipes.Profile) []recipes.EjectedModule {
	if preset.Layout() == recipes.LayoutV02 {
		return profile.Copies()
	}
	return preset.Ejects()
}

// demoModuleTime is when orb new dates the demonstration module's
// migration. It is fixed, and before the newest built-in migration, so
// every app a release creates gets recipes.DemoMigrationVersion and the
// golden apps can be compared byte for byte.
var demoModuleTime = time.Date(2026, 9, 16, 0, 0, 2, 0, time.UTC)

// generateDemoModule writes the demonstration projects module into the new
// app at dir, exactly as orb gen module writes it, at the profile's access
// rule (ADR-0090 §5, ADR-0091).
func generateDemoModule(dir, module string, profile recipes.Profile) error {
	in := moduleInput{name: recipes.DemoModule, specs: recipes.DemoFields(), scope: profile.ResourceScope()}
	plan, _, err := planModule(appInfo{dir: dir, module: module}, in, demoModuleTime)
	if err != nil {
		return err
	}
	return genplan.Apply(dir, plan)
}

// demoOwner says who the demonstration module's records belong to, for the
// line orb new prints.
func demoOwner(profile recipes.Profile) string {
	switch profile.ResourceScope() {
	case recipes.ScopeTenant:
		return profile.Vocabulary().Plural
	case recipes.ScopeUser:
		return "the signed-in user"
	}
	return "nobody: it is world-readable"
}

// produceAPI writes the app's API artefacts by running its own openapi
// command, as CI does for the golden apps: they are the app's output, not
// gorbital's templates (ADR-0090 §5). The /ops baseline the app is held to
// is the document it was created with.
func produceAPI(ctx context.Context, dir string) error {
	var out bytes.Buffer
	if err := runIn(ctx, dir, &out, "go", "run", "./cmd/api", "openapi", "--dir", "api"); err != nil {
		return fmt.Errorf("go run ./cmd/api openapi --dir api failed: %w\n%s", err, out.String())
	}
	document, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(recipes.OpenAPIPath))) //nolint:gosec // the app's own output
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.FromSlash(recipes.OpenAPIBaselinePath)), document, 0o644) //nolint:gosec // the app's own output
}

// nextSteps lists where things are in a new app of the preset in dir, and
// ends with the commands to run next. ejected reports that sign-in (and
// organisations) are in internal/modules.
func nextSteps(s styles, dir string, preset recipes.Preset, profile recipes.Profile, ejected bool) string {
	rows := [][2]string{
		{"api docs", "http://127.0.0.1:8080/docs"},
		{"traces", "orb dev --observability (needs Docker)"},
	}
	if preset.Name == "full" {
		rows = [][2]string{
			{"api docs", "http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)"},
			{"main.go", "cmd/api/main.go runs the app on gorbital.Main; your code goes in internal/modules"},
			{"modules", "orb gen module <Name> <field:type>... adds a table and its API"},
			{"emails", "http://127.0.0.1:3100/mail (the Dev Portal catches every email in development)"},
			{"admin", "admin@example.com; orb dev prints its password, 2FA key and recovery codes once"},
		}
		if ejected {
			rows = append(rows, [2]string{"sign-in", "internal/modules/auth is your code: login, registration, email verification, password reset"})
		} else {
			rows = append(rows, [2]string{"sign-in", "gorbital.dev/gorbital/authhttp, updated with go get"})
		}
		rows = append(rows, [2]string{"providers", "AUTH_PROVIDERS.md lists what to set for passkeys, Google and Apple"})
		if profile.Named() {
			if ejected {
				rows = append(rows, [2]string{"orgs", "internal/modules/orgs is your code: organisations, members, roles, invitations"})
			}
			rows = append(rows,
				[2]string{"workspaces", "every account gets a personal workspace; data lives under /v1/orgs/{orgId}"},
				[2]string{"invitations", "set orgs.invitation_url to your frontend's page before inviting people"},
			)
		}
		if ejected {
			rows = append(rows, [2]string{"fixes", "library releases don't change your copy; orb doctor says when the library's version changed, quoting its changelog"})
		}
		rows = append(rows, [][2]string{
			{"email", "Resend outside development; orb add mail switches to SMTP"},
			{"port 5432", "taken? set POSTGRES_PORT in .env and the same port in DATABASE_URL"},
			// The steps README.md lists, in the same order: without the
			// export, the app starts with no environment at all, and
			// without a key seed refuses to run.
			{"without orb", `cp .env.example .env, then set AUTH_ENCRYPTION_KEYS: echo "k1:$(openssl rand -base64 32)"`},
			{"", "docker compose up -d --wait, set -a; . ./.env; set +a,"},
			{"", "go run ./cmd/api migrate, go run ./cmd/api seed, go run ./cmd/api"},
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
func promptNew(name, module, preset, auth, scope, local *string, noGit *bool, set map[string]bool, p promptFlags, stdin io.Reader, stderr io.Writer) error {
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

	// The module path is the app name unless --module says otherwise: an API
	// is rarely imported by another module, so asking only slows the first run.
	a.answered("Go module path", cmp.Or(*module, *name))

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

	// Sign-in, then tenancy, then the tenant's own words: the two choices
	// that decide what the app gets, each shown with what it costs
	// (ADR-0090, roadmap item 54). Minimal has neither.
	if err := promptProfile(a, *preset, auth, scope, set); err != nil {
		return err
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

// promptProfile asks how much sign-in the app has, then what a tenant is,
// then the tenant's name and roles, saying what each answer costs in
// endpoints and tables.
func promptProfile(a asker, preset string, auth, scope *string, set map[string]bool) error {
	if p, _ := recipes.LookupPreset(preset, recipes.TenancySingle); p.Layout() != recipes.LayoutV02 {
		*auth, *scope = "", ""
		return nil
	}
	if *auth == "" {
		*auth = recipes.AuthFull
	}
	if !set["auth"] {
		sel := huh.NewSelect[string]().Title(a.choiceTitle("sign-in")).Options(
			huh.NewOption("none    no accounts, no sessions, no auth tables · 0 endpoints, 0 migrations", recipes.AuthNone),
			huh.NewOption("basic   email and password, plus the operators' account APIs · 29 endpoints, 8 migrations", recipes.AuthBasic),
			huh.NewOption("full    every method: 2FA, passkeys, Google, Apple, GitHub, API keys · 74 endpoints, 8 migrations", recipes.AuthFull),
		).Value(auth)
		if err := a.ask(sel, "sign-in", func() string { return *auth }); err != nil {
			return err
		}
	} else {
		a.answered("sign-in", *auth)
	}

	// A scope decides which user may act in it, so an app with no users
	// has only one answer to give (ADR-0090 §2).
	if *auth == recipes.AuthNone {
		*scope = recipes.ScopeNone
		a.answered("tenancy", "none (an app with no sign-in has no users to be members)")
		return nil
	}
	if *scope == "" {
		*scope = recipes.ScopeSingle
	}
	choice := *scope
	if !set["scope"] {
		if choice != recipes.ScopeNone && choice != recipes.ScopeSingle && choice != recipes.ScopeCustom {
			choice = recipes.DefaultScopeName
		}
		sel := huh.NewSelect[string]().Title(a.choiceTitle("tenancy")).Options(
			huh.NewOption("none    no tenants: records belong to the app · 0 endpoints, 0 tables", recipes.ScopeNone),
			huh.NewOption("single  one user base: records belong to users · 0 endpoints, 0 tables", recipes.ScopeSingle),
			huh.NewOption("named   companies or teams, with members, roles and invitations · 31 endpoints, 3 tables", recipes.DefaultScopeName),
			huh.NewOption("custom  your own membership rules, from the first commit · 0 endpoints, 2 tables", recipes.ScopeCustom),
		).Value(&choice)
		if err := a.ask(sel, "tenancy", func() string { return choice }); err != nil {
			return err
		}
		*scope = choice
		// A named scope is called whatever the app calls it (ADR-0088).
		if choice == recipes.DefaultScopeName {
			input := huh.NewInput().Title(a.title("what is a tenant called, in lowercase singular")).Inline(true).Prompt("").
				Placeholder(recipes.DefaultScopeName).Value(scope).
				Validate(func(v string) error {
					if v == "" {
						return nil
					}
					_, err := recipes.ParseProfile(*auth, v)
					return err
				})
			if err := a.ask(input, "tenant", func() string { return cmp.Or(*scope, recipes.DefaultScopeName) }); err != nil {
				return err
			}
			if *scope == "" {
				*scope = recipes.DefaultScopeName
			}
		}
	} else {
		a.answered("tenancy", *scope)
	}
	if p, err := recipes.ParseProfile(*auth, *scope); err == nil && p.Scoped() {
		a.answered("roles", p.RoleList()+" (change them in "+p.RolesFile()+")")
	}
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
func create(dir string, preset recipes.Preset, profile recipes.Profile, d recipes.Data, after func() error) ([]recipes.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	render := preset.Render
	if preset.Layout() == recipes.LayoutV02 {
		render = profile.Render
	}
	files, err := render(root, d)
	if err != nil {
		return nil, err
	}
	if err := after(); err != nil {
		return nil, err
	}
	if err := writeLock(root, newLock(preset, profile, d, files)); err != nil {
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
		return "", usageError(fmt.Sprintf("--local %s is not a gorbital checkout (no go.mod with module gorbital.dev)", local))
	}
	return filepath.ToSlash(abs), nil
}

func runIn(ctx context.Context, dir string, output io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = output, output
	return cmd.Run()
}
