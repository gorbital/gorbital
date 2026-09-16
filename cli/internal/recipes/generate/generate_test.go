package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModTemplate(t *testing.T) {
	golden := "module example.com/acme-api\n\ngo 1.26.0\n\n" +
		"require (\n\tgorbital.dev v0.0.0\n\tgorbital.dev/modules/postgres v0.0.0 // indirect\n\tgithub.com/jackc/pgx/v5 v5.11.0\n)\n\n" +
		"require gorbital.dev/modules/jobs v0.0.0\n\n" +
		"// Local development: point at this checkout.\nreplace (\n\tgorbital.dev => ../..\n\tgorbital.dev/modules/postgres => ../../modules/postgres\n)\n\n" +
		"replace gorbital.dev/modules/jobs => ../../modules/jobs\n"
	want := "module ⟦.Module⟧\n\ngo 1.26.0\n\n" +
		"require (\n\tgorbital.dev ⟦.LibraryVersion⟧\n\tgorbital.dev/modules/postgres ⟦.LibraryVersion⟧ // indirect\n\tgithub.com/jackc/pgx/v5 v5.11.0\n)\n\n" +
		"require gorbital.dev/modules/jobs ⟦.LibraryVersion⟧\n" +
		"⟦- if .Local⟧\n\nreplace (\n" +
		"\tgorbital.dev => ⟦.LocalDir \"\"⟧\n" +
		"\tgorbital.dev/modules/postgres => ⟦.LocalDir \"/modules/postgres\"⟧\n" +
		"\tgorbital.dev/modules/jobs => ⟦.LocalDir \"/modules/jobs\"⟧\n" +
		")\n⟦- end⟧\n"
	got, err := GoModTemplate([]byte(golden))
	if err != nil {
		t.Fatalf("GoModTemplate() error = %v", err)
	}
	if string(got) != want {
		t.Errorf("GoModTemplate() =\n%s\nwant\n%s", got, want)
	}
}

func TestGoModTemplateRejects(t *testing.T) {
	base := "module example.com/acme-api\n\ngo 1.26.0\n\nrequire gorbital.dev v0.0.0\n"
	for name, tt := range map[string]struct{ goMod, wantErr string }{
		"other module":          {strings.Replace(base, "example.com/acme-api", "example.com/other", 1), "the module must be"},
		"no module":             {"go 1.26.0\n\nrequire gorbital.dev v0.0.0\n", "no module line"},
		"no library":            {"module example.com/acme-api\n\nrequire github.com/x/y v1.0.0\n", "no gorbital.dev requirement"},
		"unsupported directive": {base + "exclude github.com/x/y v1.0.0\n", "unsupported directive"},
		"tool directive":        {base + "tool golang.org/x/tools/cmd/stringer\n", "unsupported directive"},
		"unterminated block":    {base + "require (\n\tgithub.com/x/y v1.0.0\n", "unterminated block"},
		"malformed requirement": {base + "require (\n\tgithub.com/x/y\n)\n", "malformed requirement"},
	} {
		if _, err := GoModTemplate([]byte(tt.goMod)); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: GoModTemplate() error = %v, want containing %q", name, err, tt.wantErr)
		}
	}
}

func TestRunRejectsLeaks(t *testing.T) {
	for name, tt := range map[string]struct{ file, content, wantErr string }{
		"repository link":    {"README.md", "See [the guide](../../docs/guides/auth.md).\n", "path into the gorbital repository"},
		"template delimiter": {"internal/app/app.go", "package app // ⟦\n", "template delimiters"},
	} {
		src := t.TempDir()
		write := func(rel, content string) {
			path := filepath.Join(src, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("go.mod", "module example.com/acme-api\n\ngo 1.26.0\n\nrequire gorbital.dev v0.0.0\n\nreplace gorbital.dev => ../..\n")
		write(tt.file, tt.content)
		dst := filepath.Join(t.TempDir(), "out")
		if err := Run(src, dst); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: Run() error = %v, want containing %q", name, err, tt.wantErr)
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("%s: Run() left templates behind after failing", name)
		}
	}
}

func TestSkippedLocalEnvironmentFiles(t *testing.T) {
	for rel, want := range map[string]bool{
		".env":                   true,
		".env.local":             true,
		"deploy/.env.production": true,
		".env.example":           false,
		"go.mod":                 true,
		"internal/app/env.go":    false,
	} {
		if got := Skipped(rel); got != want {
			t.Errorf("Skipped(%q) = %v, want %v", rel, got, want)
		}
	}
}

// TestRunLeavesGitIgnoredFilesOut: with the files GitFiles returns, a key,
// coverage output and an editor directory that git ignores next to a golden
// app never become templates, while tracked and new unignored files do.
func TestRunLeavesGitIgnoredFilesOut(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "examples", "app")
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/acme-api\n\ngo 1.26.0\n\nrequire gorbital.dev v0.0.0\n")
	write(".gitignore", "*.pem\ncoverage.out\n.idea/\n")
	write("internal/app/app.go", "package app\n")
	write("internal/app/new.go", "package app // untracked, not ignored\n")
	write("deploy/signing.pem", "-----BEGIN PRIVATE KEY-----\n")
	write("coverage.out", "mode: set\n")
	write(".idea/workspace.xml", "<project/>\n")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(repo, "none"), "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet")
	git("add", "examples/app/go.mod", "examples/app/.gitignore", "examples/app/internal/app/app.go")

	files, ok, err := GitFiles(t.Context(), src)
	if err != nil || !ok {
		t.Fatalf("GitFiles() = %v, %v, %v", files, ok, err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := Run(src, dst, OnlyFiles(files)); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for rel, want := range map[string]bool{
		"internal/app/app.go.tmpl": true,
		"internal/app/new.go.tmpl": true,
		".gitignore.tmpl":          true,
		"go.mod.tmpl":              true,
		"deploy/signing.pem.tmpl":  false,
		"coverage.out.tmpl":        false,
		".idea/workspace.xml.tmpl": false,
		"deploy":                   false,
	} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); (err == nil) != want {
			t.Errorf("template %s exists = %v, want %v", rel, err == nil, want)
		}
	}

	if _, ok, err := GitFiles(t.Context(), t.TempDir()); ok || err != nil {
		t.Errorf("GitFiles() outside a work tree = %v, %v; want not ok", ok, err)
	}
}
