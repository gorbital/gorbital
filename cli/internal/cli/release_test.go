package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"apistock.dev/cli/internal/recipes"
)

// repoRoot returns the apistock checkout these tests run in.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// TestReleaseFromCheckout reads v0.4.0's templates with git archive. v0.4.0
// is the release this branch started from, so it renders the same app.
func TestReleaseFromCheckout(t *testing.T) {
	root := repoRoot(t)
	if _, err := gitOutput(context.Background(), root, "rev-parse", "--verify", "--quiet", "v0.4.0^{commit}"); err != nil {
		t.Skip("tag v0.4.0 isn't in this checkout (git fetch --tags)")
	}
	release, cleanup, err := releaseFromCheckout(context.Background(), root, "v0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	d := recipes.Data{Name: "shop-api", Module: "example.com/shop-api"}
	got, err := release.Tree("full", recipes.TenancyMulti, recipes.MailSMTP, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 150 {
		t.Errorf("v0.4.0 tree has %d files", len(got))
	}
	if _, err := release.Tree("full", recipes.TenancySingle, recipes.MailResend, d); err != nil {
		t.Error(err)
	}

	// v0.2.0 has no multi-tenant templates.
	if _, err := gitOutput(context.Background(), root, "rev-parse", "--verify", "--quiet", "v0.2.0^{commit}"); err == nil {
		old, cleanupOld, err := releaseFromCheckout(context.Background(), root, "v0.2.0")
		if err != nil {
			t.Fatal(err)
		}
		defer cleanupOld()
		if _, err := old.Tree("full", recipes.TenancyMulti, "", d); err == nil {
			t.Error("v0.2.0 rendered a multi-tenant app")
		}
		if single, err := old.Tree("full", recipes.TenancySingle, recipes.MailResend, d); err != nil || len(single) < 100 {
			t.Errorf("v0.2.0 single-tenant tree: %d files, %v", len(single), err)
		}
	}
}

func TestReleaseFromCheckoutRejectsRefs(t *testing.T) {
	for _, ref := range []string{"--output=/tmp/x", "v0.4.0..HEAD", "HEAD@{1}", "", "v0.4.0 main", "nonexistent-tag"} {
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
		tw.Write([]byte(body))
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
		{map[string]string{"GONOSUMDB": "apistock.dev"}, false},
		{map[string]string{"GOPRIVATE": "github.com/acme,apistock.dev/cli"}, false},
		{map[string]string{"GOINSECURE": "*.dev"}, false},
		{map[string]string{"GOFLAGS": "-mod=mod -insecure"}, false},
	} {
		if err := checksumPolicy(tt.env); (err == nil) != tt.ok {
			t.Errorf("checksumPolicy(%v) = %v, want ok %v", tt.env, err, tt.ok)
		}
	}
}
