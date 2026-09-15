package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"

	"apistock.dev/cli/internal/recipes"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*$`)
)

// maxNameLength keeps app names valid as PostgreSQL role and database names
// in the Full preset's compose.yaml.
const maxNameLength = 63

type newResult struct {
	Name   string `json:"name"`
	Module string `json:"module"`
	Dir    string `json:"dir"`
	Preset string `json:"preset"`
	Files  int    `json:"files"`
}

func runNew(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	module := flags.String("module", "", "Go module path (default: the app name)")
	preset := flags.String("preset", "minimal", "preset: minimal (HTTP API, no database) or full (PostgreSQL, authentication, jobs, email, audit, ops APIs)")
	local := flags.String("local", "", "path to an apistock checkout, used through replace directives (default: the checkout you are in, if any)")
	noGit := flags.Bool("no-git", false, "don't initialise a git repository")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: aps new [<name>] [flags]\n\nIn a terminal, missing values are asked interactively; pass flags to skip them.\n\nFlags:")
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
	// Reject a bad --preset before asking anything else.
	if _, err := lookupPreset(*preset); err != nil {
		return err
	}

	// Until the library is published, apps need a checkout; use the one the
	// command runs in when none is given.
	detected := false
	if *local == "" {
		if dir := findCheckout(); dir != "" {
			*local, detected = dir, true
		}
	}

	ask := shouldPrompt(p, *asJSON, stdin, stdout)
	if ask {
		if err := promptNew(&name, module, preset, local, noGit, set, p, stdin, stderr); err != nil {
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
	chosen, err := lookupPreset(*preset)
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
	fmt.Fprintf(log, "creating %s in ./%s\n%s\n\n", name, name, s.dim.Render("preset "+chosen.Name+" · "+libraryLine(localPath, detected)))

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
				"  if apistock isn't published yet, create the app with --local <path to apistock checkout>", name, err, out.String())
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

	res := newResult{Name: name, Module: *module, Dir: name, Preset: chosen.Name, Files: len(files)}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Fprintf(stdout, "\n%s\n\n%s", s.strong.Render("created "+name), nextSteps(s, name, chosen.Name))
	return nil
}

// lookupPreset returns the preset aps new --preset name selects, or a usage
// error naming the presets that exist.
func lookupPreset(name string) (recipes.Preset, error) {
	if p, ok := recipes.LookupPreset(name); ok {
		return p, nil
	}
	if name == "custom" {
		return recipes.Preset{}, usageError("preset custom isn't available yet; use --preset minimal or --preset full")
	}
	return recipes.Preset{}, usageError(fmt.Sprintf("unknown preset %q (want %s)", name, strings.Join(recipes.PresetNames(), " or ")))
}

// nextSteps lists where things are in a new app of the preset in dir, and
// ends with the commands to run next.
func nextSteps(s styles, dir, preset string) string {
	rows := [][2]string{
		{"api docs", "http://127.0.0.1:8080/docs"},
		{"traces", "aps dev --observability (needs Docker)"},
	}
	if preset == "full" {
		rows = [][2]string{
			{"api docs", "http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)"},
			{"emails", "http://127.0.0.1:8025 (Mailpit catches every email in development)"},
			{"admin", "admin@example.com; aps dev prints its password, 2FA key and recovery codes once"},
			{"sign-in", "AUTH_PROVIDERS.md lists what to set for passkeys, Google and Apple"},
			{"email", "Resend outside development; aps add mail switches to SMTP"},
			{"port 5432", "taken? set POSTGRES_PORT in .env and the same port in DATABASE_URL"},
			{"without aps", "cp .env.example .env, docker compose up -d --wait,"},
			{"", "go run ./cmd/migrate, go run ./cmd/seed, go run ./cmd/api"},
		}
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "  %s %s\n", s.dim.Render(fmt.Sprintf("%-12s", row[0])), row[1])
	}
	fmt.Fprintf(&b, "\n  %s cd %s\n        %s\n", s.dim.Render("next:"), dir, s.accent.Render("aps dev"))
	return b.String()
}

// promptNew asks, one question at a time, for every value not given by a
// flag. Values given by flag are shown as answered lines first.
func promptNew(name, module, preset, local *string, noGit *bool, set map[string]bool, p promptFlags, stdin io.Reader, stderr io.Writer) error {
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

	if !set["local"] {
		input := huh.NewInput().Title(a.title("apistock checkout")).Inline(true).Prompt("").
			Placeholder("/path/to/apistock").Value(local).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("enter the path of your apistock checkout")
				}
				_, err := resolveLocal(s)
				return err
			})
		if err := a.ask(input, "apistock checkout", func() string { return *local }); err != nil {
			return err
		}
	} else {
		a.answered("apistock checkout", *local)
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

// libraryLine says which apistock the app is built against.
func libraryLine(local string, detected bool) string {
	switch {
	case local == "":
		return "library apistock.dev " + recipes.LibraryVersion
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
// whose go.mod declares module apistock.dev, or "".
func findCheckout() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && bytes.HasPrefix(data, []byte("module apistock.dev\n")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// create renders the preset into dir and records its files in
// apistock.lock.
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
	if err := writeLock(root, preset.Recipe, recipes.LibraryVersion, files); err != nil {
		return nil, err
	}
	return files, nil
}

func validateName(name string) error {
	switch {
	case name == "":
		return usageError("missing app name: aps new <name> (or run it in a terminal to be asked)")
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
	if err != nil || !strings.HasPrefix(string(goMod), "module apistock.dev\n") {
		return "", usageError(fmt.Sprintf("--local %s is not an apistock checkout (no go.mod with module apistock.dev)", local))
	}
	return filepath.ToSlash(abs), nil
}

func runIn(ctx context.Context, dir string, output io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = output, output
	return cmd.Run()
}
