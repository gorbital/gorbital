// Command gen regenerates every preset's recipe templates from its golden
// app. Run it with go generate from cli/internal/recipes.
//
// The golden Full apps on gorbital.Main are what orb new writes: since
// v0.2.1 they hold sign-in, and full-multi its organisations, as their own
// code in internal/modules. Their templates don't: orb new copies those
// modules from the library version the new app requires, so gen takes them
// out of a copy of each golden app (generate.Uneject), records the copy's
// api/surface.json and writes the templates from it. Then it copies the
// modules into that copy again from this checkout, as orb new does, and
// writes the result back into the golden app, so the golden apps always
// show the library's current modules and CI's diff of examples/ catches
// a golden app that doesn't.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gorbital.dev/cli/internal/cli"
	"gorbital.dev/cli/internal/recipes"
	"gorbital.dev/cli/internal/recipes/generate"
)

// repoRoot is the gorbital checkout, relative to cli/internal/recipes.
const repoRoot = "../../.."

// goldenApps maps each golden app to the template directory it generates.
var goldenApps = []struct {
	src, dst string
	// main marks golden apps on gorbital.Main, whose generated files hold
	// library text that isn't theirs to rename.
	main bool
	// preset and tenancy name the preset whose built-in modules orb new
	// ejects, for golden apps on gorbital.Main.
	preset, tenancy string
}{
	{"../../../examples/minimal", "minimal", false, "", ""},
	{"../../../examples/v0.1/full-single", "full", false, "", ""},
	{"../../../examples/v0.1/full-multi", "full-multi", false, "", ""},
	{"../../../examples/full-single", "v0.2/full", true, "full", recipes.TenancySingle},
	{"../../../examples/full-multi", "v0.2/full-multi", true, "full", recipes.TenancyMulti},
}

func main() {
	ctx := context.Background()
	for _, app := range goldenApps {
		if err := generateApp(ctx, app.src, app.dst, app.main, app.preset, app.tenancy); err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
	}
}

func generateApp(ctx context.Context, src, dst string, main bool, preset, tenancy string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Only files git tracks or would track become templates: anything
	// git-ignored next to a golden app (.env, keys, coverage output) stays
	// on this machine. Outside a git work tree, such as a source archive,
	// generate.Skipped still leaves local environment files out.
	files, ok, err := generate.GitFiles(ctx, src)
	if err != nil {
		return err
	}
	var opts []generate.Option
	if main {
		opts = append(opts, generate.KeepLibraryLiterals())
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "gen: %s isn't in a git work tree; git-ignored files other than .env files would become templates\n", src)
	}
	var ejected []recipes.EjectedModule
	if p, found := recipes.LookupPreset(preset, tenancy); found {
		ejected = p.Ejects()
	}
	if len(ejected) == 0 {
		if ok {
			opts = append(opts, generate.OnlyFiles(files))
		}
		return generate.Run(src, dst, opts...)
	}
	return generateEjected(ctx, src, dst, files, ejected, opts)
}

// generateEjected writes the templates of the golden app at src, which
// holds ejected, from a copy without them, then writes the golden app again
// from that copy with the modules ejected from this checkout.
func generateEjected(ctx context.Context, src, dst string, files map[string]bool, ejected []recipes.EjectedModule, opts []generate.Option) error {
	tmp, err := os.MkdirTemp("", "orb-gen-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	copied, err := copyApp(src, tmp, files)
	if err != nil {
		return err
	}

	// The app the templates hold: the golden app without the modules, with
	// the requirements and api/surface.json of its own code.
	var modules []generate.Ejected
	for _, e := range ejected {
		modules = append(modules, generate.Ejected{Dir: e.Dir(), Package: e.Package})
	}
	if err := generate.Uneject(tmp, generate.PlaceholderModule, modules); err != nil {
		return err
	}
	if err := goIn(ctx, tmp, "mod", "tidy"); err != nil {
		return err
	}
	if err := recordSurface(ctx, tmp); err != nil {
		return err
	}
	only := map[string]bool{}
	for p := range copied {
		if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(p))); err == nil {
			only[p] = true
		}
	}
	if err := generate.Run(tmp, dst, append(opts, generate.OnlyFiles(only))...); err != nil {
		return err
	}

	// The golden app: what orb new writes from those templates.
	checkout, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	if err := cli.EjectIntoApp(tmp, ejected, checkout); err != nil {
		return fmt.Errorf("eject into %s: %w", src, err)
	}
	if err := goIn(ctx, tmp, "mod", "tidy"); err != nil {
		return err
	}
	if err := recordSurface(ctx, tmp); err != nil {
		return err
	}
	if err := syncApp(tmp, src, copied); err != nil {
		return err
	}
	return goIn(ctx, src, "mod", "tidy")
}

// copyApp copies the golden app's files at src (only those in files, when
// it isn't nil) into dst, with its replace directives made absolute so the
// copy builds with this checkout, and returns their paths.
func copyApp(src, dst string, files map[string]bool) (map[string]bool, error) {
	abs, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	copied := map[string]bool{}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && generate.SkippedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		keep := files[rel] || rel == "go.mod" || rel == "go.sum"
		if files == nil {
			base := filepath.Base(rel)
			keep = !strings.HasPrefix(base, ".env") || base == ".env.example"
		}
		if !keep {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // a golden app's file
		if err != nil {
			return err
		}
		if rel == "go.mod" {
			data = absoluteReplaces(data, abs)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		copied[rel] = true
		return os.WriteFile(target, data, 0o644) //nolint:gosec // a copy of a golden app
	})
	return copied, err
}

// absoluteReplaces makes a go.mod's relative replace directives, relative
// to dir, absolute.
func absoluteReplaces(goMod []byte, dir string) []byte {
	lines := strings.Split(string(goMod), "\n")
	for i, line := range lines {
		before, target, ok := strings.Cut(line, "=> ")
		if ok && (strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../")) {
			lines[i] = before + "=> " + filepath.ToSlash(filepath.Join(dir, filepath.FromSlash(strings.TrimSpace(target))))
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// syncApp writes the files of the app in from, except go.mod and go.sum,
// into the golden app at to, and removes the golden app's files among old
// that from no longer has.
func syncApp(from, to string, old map[string]bool) error {
	now := map[string]bool{}
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		rel = filepath.ToSlash(rel)
		if rel == "go.mod" || rel == "go.sum" {
			return nil
		}
		now[rel] = true
		data, err := os.ReadFile(p) //nolint:gosec // gen's own copy
		if err != nil {
			return err
		}
		target := filepath.Join(to, filepath.FromSlash(rel))
		if current, err := os.ReadFile(target); err == nil && bytes.Equal(current, data) { //nolint:gosec // a golden app's file
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644) //nolint:gosec // a golden app's file
	})
	if err != nil {
		return err
	}
	for rel := range old {
		if now[rel] || rel == "go.mod" || rel == "go.sum" {
			continue
		}
		if err := os.Remove(filepath.Join(to, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return pruneEmptyDirs(to)
}

// pruneEmptyDirs removes the empty directories under dir, which removed
// files of an ejected module can leave.
func pruneEmptyDirs(dir string) error {
	var dirs []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != dir {
			if generate.SkippedDirs[d.Name()] {
				return fs.SkipDir
			}
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if entries, err := os.ReadDir(dirs[i]); err == nil && len(entries) == 0 {
			if err := os.Remove(dirs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// recordSurface records the app's api/surface.json, as its TestPublicSurface
// does with -update.
func recordSurface(ctx context.Context, dir string) error {
	return goIn(ctx, dir, "test", "./internal/modules", "-run", "^TestPublicSurface$", "-count=1", "-update")
}

func goIn(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go %s in %s: %w\n%s", strings.Join(args, " "), dir, err, out)
	}
	return nil
}
