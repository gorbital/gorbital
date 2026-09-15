package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModTemplate(t *testing.T) {
	golden := "module example.com/acme-api\n\ngo 1.26.0\n\n" +
		"require (\n\tapistock.dev v0.0.0\n\tapistock.dev/modules/postgres v0.0.0 // indirect\n\tgithub.com/jackc/pgx/v5 v5.11.0\n)\n\n" +
		"require apistock.dev/modules/jobs v0.0.0\n\n" +
		"// Local development: point at this checkout.\nreplace (\n\tapistock.dev => ../..\n\tapistock.dev/modules/postgres => ../../modules/postgres\n)\n\n" +
		"replace apistock.dev/modules/jobs => ../../modules/jobs\n"
	want := "module ⟦.Module⟧\n\ngo 1.26.0\n\n" +
		"require (\n\tapistock.dev ⟦.LibraryVersion⟧\n\tapistock.dev/modules/postgres ⟦.LibraryVersion⟧ // indirect\n\tgithub.com/jackc/pgx/v5 v5.11.0\n)\n\n" +
		"require apistock.dev/modules/jobs ⟦.LibraryVersion⟧\n" +
		"⟦- if .Local⟧\n\nreplace (\n" +
		"\tapistock.dev => ⟦.LocalDir \"\"⟧\n" +
		"\tapistock.dev/modules/postgres => ⟦.LocalDir \"/modules/postgres\"⟧\n" +
		"\tapistock.dev/modules/jobs => ⟦.LocalDir \"/modules/jobs\"⟧\n" +
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
	base := "module example.com/acme-api\n\ngo 1.26.0\n\nrequire apistock.dev v0.0.0\n"
	for name, tt := range map[string]struct{ goMod, wantErr string }{
		"other module":          {strings.Replace(base, "example.com/acme-api", "example.com/other", 1), "the module must be"},
		"no module":             {"go 1.26.0\n\nrequire apistock.dev v0.0.0\n", "no module line"},
		"no library":            {"module example.com/acme-api\n\nrequire github.com/x/y v1.0.0\n", "no apistock.dev requirement"},
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
		"repository link":    {"README.md", "See [the guide](../../docs/guides/auth.md).\n", "path into the apistock repository"},
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
		write("go.mod", "module example.com/acme-api\n\ngo 1.26.0\n\nrequire apistock.dev v0.0.0\n\nreplace apistock.dev => ../..\n")
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
