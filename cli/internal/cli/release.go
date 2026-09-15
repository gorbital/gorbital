package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"apistock.dev/cli/internal/recipes"
)

// recipesDir is where a release keeps its templates, relative to the
// apistock repository and to the apistock.dev/cli module.
const recipesDir = "cli/internal/recipes"

// cliModule is the module an apistock release publishes aps in.
const cliModule = "apistock.dev/cli"

// refPattern matches the git revisions and tags aps passes to git: never an
// option, never a range or reflog expression.
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// openRelease returns the templates of an older release: ref (a tag or
// commit) from the apistock checkout, when one is given, or version from the
// Go module proxy, verified by the checksum database (ADR-0050). The
// templates are only read and rendered as text. It is a variable so tests can
// supply releases.
var openRelease = func(ctx context.Context, checkout, ref, version string) (recipes.Release, func(), error) {
	if checkout != "" {
		return releaseFromCheckout(ctx, checkout, ref)
	}
	return releaseFromProxy(ctx, version)
}

// releaseFromCheckout extracts ref's templates from the checkout with git
// archive into a temporary directory.
func releaseFromCheckout(ctx context.Context, checkout, ref string) (recipes.Release, func(), error) {
	if !refPattern.MatchString(ref) || strings.Contains(ref, "..") {
		return recipes.Release{}, nil, fmt.Errorf("invalid release %q: use a tag such as v0.4.0 or a commit", ref)
	}
	// A CLI tag names the release when there is one; the commit is the same.
	commit, err := gitOutput(ctx, checkout, "rev-parse", "--verify", "--quiet", "cli/"+ref+"^{commit}")
	if err != nil {
		if commit, err = gitOutput(ctx, checkout, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
			return recipes.Release{}, nil, fmt.Errorf("release %s isn't in the apistock checkout %s (try git fetch --tags there)", ref, checkout)
		}
	}

	var archive, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", commit, recipesDir)
	cmd.Dir, cmd.Stdout, cmd.Stderr = checkout, &archive, &stderr
	if err := cmd.Run(); err != nil {
		return recipes.Release{}, nil, fmt.Errorf("read the templates of %s from %s: %w: %s", ref, checkout, err, strings.TrimSpace(stderr.String()))
	}
	dir, err := os.MkdirTemp("", "aps-release-")
	if err != nil {
		return recipes.Release{}, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	if err := extractTemplates(&archive, dir); err != nil {
		cleanup()
		return recipes.Release{}, nil, fmt.Errorf("read the templates of %s: %w", ref, err)
	}
	return recipes.ReleaseFS(os.DirFS(dir)), cleanup, nil
}

// extractTemplates writes the regular files under recipesDir in a tar
// archive into dir, through os.Root so no entry can write outside it.
func extractTemplates(r io.Reader, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(h.Name, recipesDir+"/")
		if !ok || h.Typeflag != tar.TypeReg || rel == "" {
			continue // directories, symlinks and anything outside the templates
		}
		if d := path.Dir(rel); d != "." {
			if err := root.MkdirAll(d, 0o755); err != nil {
				return err
			}
		}
		f, err := root.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 64<<20))
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
}

// releaseFromProxy downloads apistock.dev/cli at version into the module
// cache with go mod download, refusing when checksum verification is off for
// apistock.dev.
func releaseFromProxy(ctx context.Context, version string) (recipes.Release, func(), error) {
	if !refPattern.MatchString(version) || !strings.HasPrefix(version, "v") {
		return recipes.Release{}, nil, fmt.Errorf("invalid release version %q", version)
	}
	envJSON, err := goOutput(ctx, "env", "-json", "GOSUMDB", "GONOSUMDB", "GOPRIVATE", "GOINSECURE", "GOFLAGS")
	if err != nil {
		return recipes.Release{}, nil, err
	}
	var env map[string]string
	if err := json.Unmarshal([]byte(envJSON), &env); err != nil {
		return recipes.Release{}, nil, fmt.Errorf("read go env: %w", err)
	}
	if err := checksumPolicy(env); err != nil {
		return recipes.Release{}, nil, err
	}

	out, err := goOutput(ctx, "mod", "download", "-json", cliModule+"@"+version)
	var info struct{ Dir, Error string }
	if jsonErr := json.Unmarshal([]byte(out), &info); jsonErr != nil || info.Error != "" || err != nil {
		return recipes.Release{}, nil, fmt.Errorf("download %s@%s: %s (with an apistock checkout, pass --local <path>)", cliModule, version, strings.TrimSpace(info.Error+" "+errString(err)))
	}
	return recipes.ReleaseFS(os.DirFS(path.Join(info.Dir, "internal", "recipes"))), func() {}, nil
}

// checksumPolicy refuses Go environments that would download apistock.dev
// modules without checking them against the checksum database.
func checksumPolicy(env map[string]string) error {
	if env["GOSUMDB"] == "off" {
		return errors.New("GOSUMDB=off: aps upgrade only uses releases verified by the Go checksum database")
	}
	for _, name := range []string{"GONOSUMDB", "GOPRIVATE", "GOINSECURE"} {
		if matchesModulePrefix(env[name], cliModule) {
			return fmt.Errorf("%s covers %s: aps upgrade only uses releases verified by the Go checksum database", name, cliModule)
		}
	}
	if strings.Contains(" "+env["GOFLAGS"]+" ", " -insecure ") {
		return errors.New("GOFLAGS has -insecure: aps upgrade only uses releases verified by the Go checksum database")
	}
	return nil
}

// matchesModulePrefix reports whether a comma-separated list of glob
// patterns, as GOPRIVATE uses, matches module or one of its path prefixes.
func matchesModulePrefix(patterns, module string) bool {
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		n := strings.Count(pattern, "/") + 1
		parts := strings.SplitN(module, "/", n+1)
		if len(parts) < n {
			continue
		}
		if ok, _ := path.Match(pattern, strings.Join(parts[:n], "/")); ok {
			return true
		}
	}
	return false
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func goOutput(ctx context.Context, args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
