package recipes_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"apistock.dev/cli/internal/recipes"
)

var shopData = recipes.Data{Name: "shop-api", Module: "example.com/shop-api", LibraryVersion: recipes.LibraryVersion}

// TestTreeMatchesRender: the in-memory tree an upgrade rebuilds is exactly
// what aps new writes, for every preset.
func TestTreeMatchesRender(t *testing.T) {
	for _, golden := range goldenApps {
		t.Run(golden.templates, func(t *testing.T) {
			mail := ""
			if golden.preset == "full" {
				mail = recipes.MailResend
			}
			tree, err := recipes.Embedded().Tree(golden.preset, golden.tenancy, mail, shopData)
			if err != nil {
				t.Fatal(err)
			}
			dir, files := renderInto(t, golden.preset, golden.tenancy, shopData)
			if len(tree) != len(files) {
				t.Errorf("tree has %d files, Render wrote %d", len(tree), len(files))
			}
			for _, f := range files {
				written, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
				if content, ok := tree[f.Path]; !ok || !bytes.Equal(content, written) {
					t.Errorf("tree's %s differs from the written file", f.Path)
				}
			}
		})
	}
}

// TestTreeFromDirectory: templates read from a directory, as aps upgrade
// reads an older release, render the same tree as the embedded ones. This
// package's directory has a release's layout.
func TestTreeFromDirectory(t *testing.T) {
	want, err := recipes.Embedded().Tree("full", recipes.TenancyMulti, recipes.MailSMTP, shopData)
	if err != nil {
		t.Fatal(err)
	}
	got, err := recipes.ReleaseFS(os.DirFS(".")).Tree("full", recipes.TenancyMulti, recipes.MailSMTP, shopData)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.EqualFunc(got, want, bytes.Equal) {
		t.Error("tree rendered from the directory differs from the embedded tree")
	}
}

func TestTreeWithSMTP(t *testing.T) {
	tree, err := recipes.Embedded().Tree("full", recipes.TenancySingle, recipes.MailSMTP, shopData)
	if err != nil {
		t.Fatal(err)
	}
	smtp, _ := recipes.RenderMail(recipes.MailSMTP, shopData.Module)
	if !bytes.Equal(tree[recipes.InfraMailPath], smtp.InfraMail) {
		t.Errorf("%s is not the SMTP recipe", recipes.InfraMailPath)
	}
	if !bytes.Equal(tree[recipes.InfraMailTestPath], smtp.InfraMailTest) {
		t.Errorf("%s is not the SMTP recipe", recipes.InfraMailTestPath)
	}
	if example := string(tree[".env.example"]); !strings.Contains(example, "\nSMTP_HOST=\n") || strings.Contains(example, "RESEND_API_KEY") {
		t.Errorf(".env.example doesn't hold the SMTP block alone:\n%s", example)
	}
	if manifest := string(tree["apistock.yaml"]); !strings.HasSuffix(manifest, "\nmail: smtp\n") {
		t.Errorf("apistock.yaml = %s", manifest)
	}
}

func TestTreeErrors(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		release                recipes.Release
		preset, tenancy, email string
	}{
		{"unknown preset", recipes.Embedded(), "custom", recipes.TenancySingle, ""},
		{"email on minimal", recipes.Embedded(), "minimal", recipes.TenancySingle, recipes.MailSMTP},
		{"unknown provider", recipes.Embedded(), "full", recipes.TenancySingle, "sendgrid"},
		// Releases before v0.4.0 have no multi-tenant templates.
		{"tree missing from release", recipes.ReleaseFS(fstest.MapFS{"full/README.md.tmpl": {Data: []byte("# ⟦.Name⟧\n")}}), "full", recipes.TenancyMulti, ""},
	} {
		if _, err := tt.release.Tree(tt.preset, tt.tenancy, tt.email, shopData); err == nil {
			t.Errorf("%s: Tree(%s, %s, %q) error = nil", tt.name, tt.preset, tt.tenancy, tt.email)
		}
	}
}
