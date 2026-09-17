package recipes_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gorbital.dev/cli/internal/recipes"
)

var shopData = recipes.Data{Name: "shop-api", Module: "example.com/shop-api", LibraryVersion: recipes.LibraryVersion}

// TestTreeMatchesRender: the in-memory tree an upgrade rebuilds is exactly
// what orb new writes, for every preset.
func TestTreeMatchesRender(t *testing.T) {
	for _, golden := range goldenApps {
		t.Run(golden.templates, func(t *testing.T) {
			mail := ""
			if golden.preset == "full" {
				mail = recipes.MailResend
			}
			p, _ := recipes.LookupPreset(golden.preset, golden.tenancy)
			if golden.layout != p.Layout() {
				t.Skipf("orb new writes the %s layout", p.Layout())
			}
			tree, err := recipes.Embedded().Tree(golden.preset, golden.tenancy, golden.layout, mail, shopData)
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

// TestTreeFromDirectory: templates read from a directory, as orb upgrade
// reads an older release, render the same tree as the embedded ones. This
// package's directory has a release's layout.
func TestTreeFromDirectory(t *testing.T) {
	for _, layout := range []string{recipes.LayoutV01, recipes.LayoutV02} {
		want, err := recipes.Embedded().Tree("full", recipes.TenancyMulti, layout, recipes.MailSMTP, shopData)
		if err != nil {
			t.Fatal(err)
		}
		got, err := recipes.ReleaseFS(os.DirFS(".")).Tree("full", recipes.TenancyMulti, layout, recipes.MailSMTP, shopData)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.EqualFunc(got, want, bytes.Equal) {
			t.Errorf("%s tree rendered from the directory differs from the embedded tree", layout)
		}
	}
}

func TestMainTreeWithSMTP(t *testing.T) {
	tree, err := recipes.Embedded().Tree("full", recipes.TenancyMulti, recipes.LayoutV02, recipes.MailSMTP, shopData)
	if err != nil {
		t.Fatal(err)
	}
	smtp, _ := recipes.RenderMail(recipes.MailSMTP, shopData.Module)
	if !bytes.Equal(tree[recipes.MainMailPath], smtp.MainMail) || !strings.Contains(string(smtp.MainMail), "opshttp.ProviderSMTP") {
		t.Errorf("%s is not the SMTP recipe:\n%s", recipes.MainMailPath, tree[recipes.MainMailPath])
	}
	if _, ok := tree[recipes.InfraMailPath]; ok {
		t.Errorf("the v0.2 tree has %s", recipes.InfraMailPath)
	}
	if example := string(tree[".env.example"]); !strings.Contains(example, "\nSMTP_HOST=\n") || strings.Contains(example, "RESEND_API_KEY") {
		t.Errorf(".env.example doesn't hold the SMTP block alone:\n%s", example)
	}
	if manifest := string(tree["gorbital.yaml"]); !strings.HasSuffix(manifest, "\nmail: smtp\n") {
		t.Errorf("gorbital.yaml = %s", manifest)
	}
}

func TestTreeWithSMTP(t *testing.T) {
	tree, err := recipes.Embedded().Tree("full", recipes.TenancySingle, recipes.LayoutV01, recipes.MailSMTP, shopData)
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
	if manifest := string(tree["gorbital.yaml"]); !strings.HasSuffix(manifest, "\nmail: smtp\n") {
		t.Errorf("gorbital.yaml = %s", manifest)
	}
}

func TestTreeErrors(t *testing.T) {
	for _, tt := range []struct {
		name                           string
		release                        recipes.Release
		preset, tenancy, layout, email string
	}{
		{"unknown preset", recipes.Embedded(), "custom", recipes.TenancySingle, recipes.LayoutV01, ""},
		{"email on minimal", recipes.Embedded(), "minimal", recipes.TenancySingle, recipes.LayoutV01, recipes.MailSMTP},
		{"unknown provider", recipes.Embedded(), "full", recipes.TenancySingle, recipes.LayoutV01, "sendgrid"},
		{"unknown provider in v0.2", recipes.Embedded(), "full", recipes.TenancySingle, recipes.LayoutV02, "sendgrid"},
		// Early development builds had no multi-tenant templates.
		{"tree missing from release", recipes.ReleaseFS(fstest.MapFS{"full/README.md.tmpl": {Data: []byte("# ⟦.Name⟧\n")}}), "full", recipes.TenancyMulti, recipes.LayoutV01, ""},
		// Releases before v0.2 have no v0.2 layout.
		{"layout missing from release", recipes.ReleaseFS(fstest.MapFS{"full/README.md.tmpl": {Data: []byte("# ⟦.Name⟧\n")}}), "full", recipes.TenancySingle, recipes.LayoutV02, ""},
	} {
		if _, err := tt.release.Tree(tt.preset, tt.tenancy, tt.layout, tt.email, shopData); err == nil {
			t.Errorf("%s: Tree(%s, %s, %q) error = nil", tt.name, tt.preset, tt.tenancy, tt.email)
		}
	}
}
