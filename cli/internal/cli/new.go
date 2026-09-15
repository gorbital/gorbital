package cli

import (
	"bytes"
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
		ok, err := confirm("Create this app?", newSummary(name, *module, chosen.Name, localPath, !*noGit), p, stdin, stderr)
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
	} else if detected && !*asJSON {
		fmt.Fprintf(stderr, "Using the apistock checkout at %s (pass --local to choose another)\n", localPath)
	}

	if err := os.Mkdir(name, 0o755); err != nil {
		return err
	}

	files, err := create(name, chosen, recipes.Data{Name: name, Module: *module, LibraryVersion: recipes.LibraryVersion, Local: localPath})
	if err != nil {
		return errors.Join(err, os.RemoveAll(name)) // we created the directory; remove the partial app
	}

	if !*skipTidy {
		if err := runIn(ctx, name, stderr, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("created %s, but go mod tidy failed: %w\n"+
				"  if apistock isn't published yet, create the app with --local <path to apistock checkout>", name, err)
		}
	}
	if !*noGit {
		if _, err := exec.LookPath("git"); err == nil {
			if err := runIn(ctx, name, stderr, "git", "init", "--quiet"); err != nil {
				return fmt.Errorf("created %s, but git init failed: %w", name, err)
			}
		}
	}

	res := newResult{Name: name, Module: *module, Dir: name, Preset: chosen.Name, Files: len(files)}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Fprintf(stdout, "✓ Created %s (%s preset, %d files)\n\n%s", name, chosen.Name, len(files), nextSteps(name, chosen.Name))
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

// nextSteps tells how to run a new app of the preset in dir.
func nextSteps(dir, preset string) string {
	if preset != "full" {
		return fmt.Sprintf("  cd %s\n  aps dev\n\n  API docs: http://127.0.0.1:8080/docs\n  Traces and logs: aps dev --observability (needs Docker)\n", dir)
	}
	return fmt.Sprintf(`  cd %s
  aps dev      # PostgreSQL and Mailpit in Docker, migrations, seed data, live reload

  API docs:       http://127.0.0.1:8080/docs
  Email inbox:    http://127.0.0.1:8025 (Mailpit catches every email in development)
  Administrator:  admin@example.com; aps dev prints its password once, on the first run

  Without the apistock CLI: cp .env.example .env, docker compose up -d --wait,
  then go run ./cmd/migrate, go run ./cmd/seed and go run ./cmd/api.
  Port 5432 already in use? Set POSTGRES_PORT in .env, and the same port in DATABASE_URL.
  Email goes through Resend outside development; run aps add mail to use SMTP instead.
`, dir)
}

// promptNew asks for every value not given by a flag.
func promptNew(name, module, preset, local *string, noGit *bool, set map[string]bool, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	var fields []huh.Field
	if *name == "" {
		fields = append(fields, huh.NewInput().Title("App name").
			Description("Lowercase letters, digits and hyphens, such as my-api. It becomes the directory name.").
			Placeholder("my-api").Value(name).Validate(validateName))
	}
	if !set["module"] {
		fields = append(fields, huh.NewInput().Title("Go module path").
			Description("Where the code will live, such as github.com/you/my-api. Leave empty to use the app name.").
			Placeholder("github.com/you/my-api").Value(module).
			Validate(func(s string) error {
				if s == "" {
					return nil
				}
				return validateModule(s)
			}))
	}
	if !set["preset"] {
		fields = append(fields, huh.NewSelect[string]().Title("Preset").Options(
			huh.NewOption("Minimal: HTTP API with configuration, telemetry, health checks and docs; no database", "minimal"),
			huh.NewOption("Full: PostgreSQL, authentication, jobs, email, audit and ops APIs; needs Docker", "full"),
		).Value(preset))
	}
	if !set["local"] {
		fields = append(fields, huh.NewInput().Title("apistock checkout").
			Description("The library isn't published yet, so apps use a local copy of the apistock repository.").
			Placeholder("/path/to/apistock").Value(local).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("enter the path of your apistock checkout")
				}
				_, err := resolveLocal(s)
				return err
			}))
	}
	gitInit := !*noGit
	if !set["no-git"] {
		fields = append(fields, huh.NewConfirm().Title("Initialise a git repository?").
			Affirmative("Yes").Negative("No").Value(&gitInit))
	}
	if len(fields) == 0 {
		return nil
	}
	if err := runForm(huh.NewForm(huh.NewGroup(fields...)), p, stdin, stderr); err != nil {
		return err
	}
	*noGit = !gitInit
	return nil
}

func newSummary(name, module, preset, local string, git bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  App:     %s (%s preset)\n", name, preset)
	fmt.Fprintf(&b, "  Module:  %s\n", module)
	if local != "" {
		fmt.Fprintf(&b, "  Library: apistock checkout at %s\n", local)
	}
	gitLine := "don't initialise"
	if git {
		gitLine = "initialise a repository"
	}
	fmt.Fprintf(&b, "  Git:     %s", gitLine)
	return b.String()
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
