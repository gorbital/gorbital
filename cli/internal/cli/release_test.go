package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// repoRoot returns the gorbital checkout these tests run in.
// repoAbs is repoRoot as an absolute path, for orb new --local.
func repoAbs(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Development commits TestReleaseFromCheckout reads. Their private
// development tags were removed before the first public release, v0.1.0, so
// the test names the commits.
const (
	// multiTenantCommit is the commit this branch started from, so it
	// renders the same app.
	multiTenantCommit = "0004f6c615e696f9e516ed8d6d9214c78469d192"
	// singleTenantCommit predates the multi-tenant templates.
	singleTenantCommit = "c830679be843a4e5069ac68620eb93e86ec5e34a"
)

// TestReleaseFromCheckout reads a development commit's templates with git
// archive.
func TestReleaseFromCheckout(t *testing.T) {
	root := repoRoot(t)
	if _, err := gitOutput(context.Background(), root, "rev-parse", "--verify", "--quiet", multiTenantCommit+"^{commit}"); err != nil {
		t.Skipf("commit %s isn't in this checkout (a shallow clone? git fetch --unshallow)", multiTenantCommit)
	}
	release, cleanup, err := releaseFromCheckout(context.Background(), root, multiTenantCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	d := recipes.Data{Name: "shop-api", Module: "example.com/shop-api"}
	got, err := release.Tree("full", recipes.TenancyMulti, recipes.LayoutV01, recipes.MailSMTP, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 150 {
		t.Errorf("%s tree has %d files", multiTenantCommit, len(got))
	}
	if _, err := release.Tree("full", recipes.TenancySingle, recipes.LayoutV01, recipes.MailResend, d); err != nil {
		t.Error(err)
	}

	// The earlier commit has no multi-tenant templates.
	if _, err := gitOutput(context.Background(), root, "rev-parse", "--verify", "--quiet", singleTenantCommit+"^{commit}"); err == nil {
		old, cleanupOld, err := releaseFromCheckout(context.Background(), root, singleTenantCommit)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanupOld()
		if _, err := old.Tree("full", recipes.TenancyMulti, recipes.LayoutV01, "", d); err == nil {
			t.Errorf("%s rendered a multi-tenant app", singleTenantCommit)
		}
		if single, err := old.Tree("full", recipes.TenancySingle, recipes.LayoutV01, recipes.MailResend, d); err != nil || len(single) < 100 {
			t.Errorf("%s single-tenant tree: %d files, %v", singleTenantCommit, len(single), err)
		}
	}
}

func TestReleaseFromCheckoutRejectsRefs(t *testing.T) {
	for _, ref := range []string{"--output=/tmp/x", "v0.1.0..HEAD", "HEAD@{1}", "", "v0.1.0 main", "nonexistent-tag"} {
		if _, _, err := releaseFromCheckout(context.Background(), repoRoot(t), ref); err == nil {
			t.Errorf("releaseFromCheckout(%q) error = nil", ref)
		}
	}
}

func TestExtractTemplatesStaysInside(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, typ byte, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: typ, Size: int64(len(body)), Mode: 0o644, Linkname: "/etc/passwd"}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add(recipesDir+"/full/README.md.tmpl", tar.TypeReg, "# ⟦.Name⟧\n")
	add(recipesDir+"/../../escape.txt", tar.TypeReg, "escaped")
	add(recipesDir+"/full/link", tar.TypeSymlink, "")
	add("other/file.txt", tar.TypeReg, "outside the templates")
	tw.Close()

	dir := t.TempDir()
	err := extractTemplates(&buf, dir)
	if got := readFile(t, filepath.Join(dir, "full", "README.md.tmpl")); got != "# ⟦.Name⟧\n" {
		t.Errorf("template = %q", got)
	}
	if err == nil {
		t.Error("an entry escaping the directory was accepted")
	}
	for _, p := range []string{filepath.Join(dir, "full", "link"), filepath.Join(dir, "other"), filepath.Join(filepath.Dir(dir), "escape.txt")} {
		if _, statErr := os.Lstat(p); statErr == nil {
			t.Errorf("%s was written", p)
		}
	}
}

func TestChecksumPolicy(t *testing.T) {
	for _, tt := range []struct {
		env map[string]string
		ok  bool
	}{
		{map[string]string{"GOSUMDB": "sum.golang.org"}, true},
		{map[string]string{"GOPRIVATE": "github.com/acme/*,gitlab.com"}, true},
		{map[string]string{"GOSUMDB": "off"}, false},
		{map[string]string{"GONOSUMDB": "gorbital.dev"}, false},
		{map[string]string{"GOPRIVATE": "github.com/acme,gorbital.dev/cli"}, false},
		{map[string]string{"GOINSECURE": "*.dev"}, false},
		{map[string]string{"GOFLAGS": "-mod=mod -insecure"}, false},
		// Go ignores a trailing slash in these patterns (CLI-3).
		{map[string]string{"GONOSUMDB": "gorbital.dev/"}, false},
		{map[string]string{"GOPRIVATE": "gorbital.dev/cli/"}, false},
		{map[string]string{"GOINSECURE": "*.dev/"}, false},
		{map[string]string{"GOSUMDB": "sum.golang.org https://proxy.example.com/sumdb"}, true},
		{map[string]string{"GOSUMDB": "sum.golang.google.cn"}, true},
		{map[string]string{"GOSUMDB": "sum.example.com+abcdef+AAAA"}, false},
	} {
		if err := checksumPolicy(tt.env); (err == nil) != tt.ok {
			t.Errorf("checksumPolicy(%v) = %v, want ok %v", tt.env, err, tt.ok)
		}
	}
}

// TestMatchesModulePrefix compares with the go command's matching
// (golang.org/x/mod/module.MatchPrefixPatterns) for gorbital.dev/cli.
func TestMatchesModulePrefix(t *testing.T) {
	for _, tt := range []struct {
		patterns string
		want     bool
	}{
		{"gorbital.dev", true}, {"gorbital.dev/", true}, {"gorbital.dev/cli", true}, {"gorbital.dev/cli/", true},
		{"gorbital.dev//", false}, {"*.dev/", true}, {"*", true}, {"gorbital.dev,", true}, {",gorbital.dev/", true},
		{"gorbital.dev/*", true}, {"gorbital.dev/cli/internal", false}, {"gorbital.dev/modules", false},
		{"gorbital", false}, {"github.com/acme/*,gitlab.com", false}, {"", false}, {"/", false},
	} {
		if got := matchesModulePrefix(tt.patterns, cliModule); got != tt.want {
			t.Errorf("matchesModulePrefix(%q) = %v, want %v", tt.patterns, got, tt.want)
		}
	}
}

// gitRepo creates a git repository at dir holding files, committed.
func gitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), content)
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "-A"}, {"commit", "--quiet", "-m", "Commit"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestReleaseCheckoutRefusesCheckoutInsideApp: a "checkout" committed into
// the app's own repository, named by its go.mod or --local, never supplies
// the earlier templates (CLI-5).
func TestReleaseCheckoutRefusesCheckoutInsideApp(t *testing.T) {
	isolateGit(t)
	app := filepath.Join(t.TempDir(), "shop-api")
	gitRepo(t, app, map[string]string{
		"go.mod":                             "module example.com/shop-api\n\ngo 1.26.0\n\nrequire gorbital.dev v0.0.0\n\nreplace gorbital.dev => ./third_party/gorbital\n",
		"third_party/gorbital/go.mod":        "module gorbital.dev\n\ngo 1.26.0\n",
		"third_party/gorbital/cli/README.md": "templates an attacker controls\n",
	})
	t.Chdir(app)

	if got, err := releaseCheckout(t.Context(), app, ""); err != nil || got != "" {
		t.Errorf("releaseCheckout() = %q, %v; want no checkout", got, err)
	}
	if got, err := releaseCheckout(t.Context(), app, "third_party/gorbital"); err == nil || !strings.Contains(err.Error(), "inside the app's git repository") {
		t.Errorf("releaseCheckout(--local inside the app) = %q, %v; want refused", got, err)
	}
	if _, _, err := releaseFromCheckout(t.Context(), filepath.Join(app, "third_party", "gorbital"), "HEAD"); err == nil || !strings.Contains(err.Error(), "isn't the top of its git repository") {
		t.Errorf("releaseFromCheckout(subdirectory of the app) error = %v; want refused", err)
	}

	// A checkout of its own next to the app is used.
	outside := filepath.Join(filepath.Dir(app), "gorbital")
	gitRepo(t, outside, map[string]string{"go.mod": "module gorbital.dev\n\ngo 1.26.0\n"})
	if got, err := releaseCheckout(t.Context(), app, outside); err != nil || got != filepath.ToSlash(outside) {
		t.Errorf("releaseCheckout(--local outside the app) = %q, %v", got, err)
	}
}

// TestReleaseFromCheckoutNeedsAGorbitalCommit: the release must be a commit
// whose go.mod is the gorbital repository's, not some other project's.
func TestReleaseFromCheckoutNeedsAGorbitalCommit(t *testing.T) {
	isolateGit(t)
	repo := filepath.Join(t.TempDir(), "gorbital")
	gitRepo(t, repo, map[string]string{
		"go.mod":                            "module example.com/shop-api\n\ngo 1.26.0\n",
		recipesDir + "/full/README.md.tmpl": "# ⟦.Name⟧\n",
	})
	writeFile(t, filepath.Join(repo, "go.mod"), "module gorbital.dev\n\ngo 1.26.0\n") // uncommitted
	if _, _, err := releaseFromCheckout(t.Context(), repo, "HEAD"); err == nil || !strings.Contains(err.Error(), "isn't a commit of the gorbital repository") {
		t.Fatalf("releaseFromCheckout(an app's commit) error = %v; want refused", err)
	}

	if out, err := exec.Command("git", "-C", repo, "commit", "--quiet", "-am", "Rename").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	_, cleanup, err := releaseFromCheckout(t.Context(), repo, "HEAD")
	if err != nil {
		t.Fatalf("releaseFromCheckout(a gorbital commit) error = %v", err)
	}
	cleanup()
}
