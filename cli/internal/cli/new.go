package cli

import (
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

	"apistock.dev/cli/internal/recipes"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*$`)
)

const maxNameLength = 63

type newResult struct {
	Name   string `json:"name"`
	Module string `json:"module"`
	Dir    string `json:"dir"`
	Preset string `json:"preset"`
	Files  int    `json:"files"`
}

func runNew(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	module := flags.String("module", "", "Go module path (default: the app name)")
	preset := flags.String("preset", "minimal", "preset: minimal (full and custom arrive in v0.2)")
	local := flags.String("local", "", "path to an apistock checkout, used through replace directives")
	noGit := flags.Bool("no-git", false, "don't initialise a git repository")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: aps new <name> [flags]")
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

	if err := validateName(name); err != nil {
		return err
	}
	if *module == "" {
		*module = name
	}
	if err := validateModule(*module); err != nil {
		return err
	}
	switch *preset {
	case "minimal":
	case "full", "custom":
		return usageError(fmt.Sprintf("preset %q arrives in v0.2; use --preset minimal", *preset))
	default:
		return usageError(fmt.Sprintf("unknown preset %q (want minimal)", *preset))
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
	if err := os.Mkdir(name, 0o755); err != nil {
		return err
	}

	files, err := create(name, recipes.Data{Name: name, Module: *module, LibraryVersion: recipes.LibraryVersion, Local: localPath})
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

	res := newResult{Name: name, Module: *module, Dir: name, Preset: *preset, Files: len(files)}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Fprintf(stdout, "✓ Created %s (%s preset, %d files)\n\n  cd %s\n  aps dev\n\n  API docs: http://127.0.0.1:8080/docs\n",
		name, *preset, len(files), name)
	return nil
}

func create(dir string, d recipes.Data) ([]recipes.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	files, err := recipes.RenderMinimal(root, d)
	if err != nil {
		return nil, err
	}
	if err := writeLock(root, recipes.MinimalName, recipes.LibraryVersion, files); err != nil {
		return nil, err
	}
	return files, nil
}

func validateName(name string) error {
	switch {
	case name == "":
		return usageError("missing app name: aps new <name>")
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
