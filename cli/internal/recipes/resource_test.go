package recipes

import (
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden apps' projects modules from the resource templates")

const (
	goldenFullSingle = "../../../examples/full-single"
	goldenFullMulti  = "../../../examples/full-multi"
)

// goldenResources are the golden apps whose projects module orb gen resource
// reproduces, with the scope and migration version that produce it.
var goldenResources = []struct{ dir, scope, migration string }{
	{goldenFullSingle, ScopeUser, "20260915000002"},
	{goldenFullMulti, ScopeOrg, "20260916000002"},
}

func projectsData(t *testing.T, scope, migration string) ResourceData {
	t.Helper()
	fields, err := ParseFields([]string{"name:string:unique", "description:text", "status:enum(active,archived)"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewResourceData("example.com/acme-api", "Project", fields, ResourceOptions{Migration: migration, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestResourceMatchesGoldenApp checks that
//
//	orb gen resource Project name:string:unique description:text 'status:enum(active,archived)'
//
// reproduces examples/full-single's projects module exactly (ADR-0039), and
// with --scope org examples/full-multi's (ADR-0048). After changing the
// templates, run go test -run TestResourceMatchesGoldenApp -update and
// review the diff of both golden apps.
func TestResourceMatchesGoldenApp(t *testing.T) {
	for _, golden := range goldenResources {
		t.Run(golden.scope, func(t *testing.T) {
			d := projectsData(t, golden.scope, golden.migration)
			files, err := RenderResource(d)
			if err != nil {
				t.Fatalf("RenderResource() error = %v", err)
			}
			for _, f := range files {
				target := filepath.Join(golden.dir, f.Path)
				if *updateGolden {
					if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(target, f.Content, 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(target)
				if err != nil {
					t.Errorf("read golden %s: %v", f.Path, err)
					continue
				}
				if string(f.Content) != string(want) {
					t.Errorf("generated %s differs from %s (run with -update and review the diff)", f.Path, golden.dir)
				}
			}

			// The module's line in modules.go is the one orb gen resource adds.
			modulesGo := filepath.Join(golden.dir, "internal", "app", "modules.go")
			modules, err := os.ReadFile(modulesGo)
			if err != nil {
				t.Fatal(err)
			}
			without := strings.Replace(string(modules), "\t\t"+d.ModulesLine()+"\n", "", 1)
			if without == string(modules) {
				t.Fatalf("modules.go has no %q line", d.ModulesLine())
			}
			if got, err := InsertAfterAnchor([]byte(without), ModulesAnchor, d.ModulesLine()); err != nil || string(got) != string(modules) {
				t.Errorf("inserting %q into modules.go = %v; want the golden modules.go:\n%s", d.ModulesLine(), err, got)
			}
			if !d.Org {
				return
			}

			// So is the line in permissions.go that gives org roles its permissions.
			permissionsGo := filepath.Join(golden.dir, "internal", "app", "permissions.go")
			permissions, err := os.ReadFile(permissionsGo)
			if err != nil {
				t.Fatal(err)
			}
			without = strings.Replace(string(permissions), "\t\t"+d.PermissionsLine()+"\n", "", 1)
			if without == string(permissions) {
				t.Fatalf("permissions.go has no %q line", d.PermissionsLine())
			}
			if got, err := InsertAfterAnchor([]byte(without), OrgPermissionsAnchor, d.PermissionsLine()); err != nil || string(got) != string(permissions) {
				t.Errorf("inserting %q into permissions.go = %v; want the golden permissions.go:\n%s", d.PermissionsLine(), err, got)
			}
		})
	}
}

// TestGeneratedResourcesPass generates two more resources into a copy of
// each golden app, one with several unique and enum fields and one with
// neither, in the golden app's scope, then vets the app and runs their tests.
func TestGeneratedResourcesPass(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests copies of the golden apps")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	for _, g := range goldenResources {
		t.Run(g.scope, func(t *testing.T) { generatedResourcesPass(t, g.dir, g.scope) })
	}
}

func generatedResourcesPass(t *testing.T, goldenDir, scope string) {
	golden, err := filepath.Abs(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Dir(filepath.Dir(golden))
	dir := t.TempDir()
	if err := copyTree(golden, dir); err != nil {
		t.Fatal(err)
	}
	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	goMod = []byte(strings.ReplaceAll(string(goMod), " => ../..", " => "+repo))
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0o644); err != nil {
		t.Fatal(err)
	}

	modulesGo := filepath.Join(dir, "internal", "app", "modules.go")
	modules, err := os.ReadFile(modulesGo)
	if err != nil {
		t.Fatal(err)
	}
	permissionsGo := filepath.Join(dir, "internal", "app", "permissions.go")
	permissions, err := os.ReadFile(permissionsGo)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		name      string
		fields    []string
		migration string
	}{
		// Versions far in the future sort after every golden app migration.
		{"Customer", []string{"email:string:unique", "full_name:string", "account_code:string:unique", "notes:text", "tier:enum(free,pro,enterprise)", "region:enum(eu,us)"}, "20990101000001"},
		{"Note", []string{"title:string", "body:text"}, "20990101000002"},
	} {
		fields, err := ParseFields(r.fields)
		if err != nil {
			t.Fatal(err)
		}
		d, err := NewResourceData("example.com/acme-api", r.name, fields, ResourceOptions{Migration: r.migration, Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		files, err := RenderResource(d)
		if err != nil {
			t.Fatalf("RenderResource(%s) error = %v", r.name, err)
		}
		for _, f := range files {
			path := filepath.Join(dir, f.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if modules, err = InsertAfterAnchor(modules, ModulesAnchor, d.ModulesLine()); err != nil {
			t.Fatal(err)
		}
		if d.Org {
			if permissions, err = InsertAfterAnchor(permissions, OrgPermissionsAnchor, d.PermissionsLine()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(modulesGo, modules, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(permissionsGo, permissions, 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(stdout *os.File, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Stdout = stdout
		out := &strings.Builder{}
		cmd.Stderr = out
		if stdout == nil {
			cmd.Stdout = out
		}
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
	}
	run(nil, "go", "vet", "./...")
	spec, err := os.Create(filepath.Join(dir, "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	run(spec, "go", "run", "./cmd/api", "openapi")
	if err := spec.Close(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GORBITAL_TEST_DATABASE_URL") == "" {
		t.Skip("vetted the generated resources; set GORBITAL_TEST_DATABASE_URL to run their tests")
	}
	run(nil, "go", "test", "-count=1", "./internal/modules/customers/...", "./internal/modules/notes/...", "./internal/app/...")
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "bin" || d.Name() == ".orb" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func TestResourceNames(t *testing.T) {
	fields, err := ParseFields([]string{"title:string"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, plural                                  string
		wantPlural, wantPackage, wantTable, wantRoute string
		wantPrefix, wantHuman                         string
	}{
		{"Project", "", "Projects", "projects", "projects", "projects", "prj", "project"},
		{"Category", "", "Categories", "categories", "categories", "categories", "ctg", "category"},
		{"OrderItem", "", "OrderItems", "orderitems", "order_items", "order-items", "ord", "order item"},
		{"order-item", "", "OrderItems", "orderitems", "order_items", "order-items", "ord", "order item"},
		{"Box", "", "Boxes", "boxes", "boxes", "boxes", "box", "box"},
		{"Key", "", "Keys", "keys", "keys", "keys", "key", "key"},
		{"Person", "People", "People", "people", "people", "people", "prs", "person"},
		{"Idea", "", "Ideas", "ideas", "ideas", "ideas", "ide", "idea"},
	}
	for _, tt := range tests {
		d, err := NewResourceData("example.com/x", tt.name, fields, ResourceOptions{Plural: tt.plural, Migration: "20260915000000"})
		if err != nil {
			t.Errorf("NewResourceData(%q) error = %v", tt.name, err)
			continue
		}
		got := []string{d.Plural, d.Package, d.Table, d.Route, d.IDPrefix, d.Human}
		want := []string{tt.wantPlural, tt.wantPackage, tt.wantTable, tt.wantRoute, tt.wantPrefix, tt.wantHuman}
		if strings.Join(got, " | ") != strings.Join(want, " | ") {
			t.Errorf("NewResourceData(%q) names = %v, want %v", tt.name, got, want)
		}
	}
}

func TestResourceValidation(t *testing.T) {
	title, err := ParseFields([]string{"title:string"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		fields  []string
		opts    ResourceOptions
		wantErr string
	}{
		{"Project", nil, ResourceOptions{}, "at least one field"},
		{"Project", []string{"notes:text"}, ResourceOptions{}, "at least one string field"},
		{"Project", []string{"title"}, ResourceOptions{}, "write it as name:type"},
		{"Project", []string{"Title:string"}, ResourceOptions{}, "snake_case"},
		{"Project", []string{"title:number"}, ResourceOptions{}, "type must be string, text or enum"},
		{"Project", []string{"title:string", "title:text"}, ResourceOptions{}, "more than once"},
		{"Project", []string{"id:string"}, ResourceOptions{}, "reserved"},
		{"Project", []string{"order:string"}, ResourceOptions{}, "reserved"},
		{"Project", []string{"name_sort:string"}, ResourceOptions{}, "reserved"},
		{"Project", []string{"title:string", "notes:text:unique"}, ResourceOptions{}, "only string fields can be unique"},
		{"Project", []string{"title:string:primary"}, ResourceOptions{}, "unknown option"},
		{"Project", []string{"title:string", "status:enum(open)"}, ResourceOptions{}, "2 to 20 values"},
		{"Project", []string{"title:string", "status:enum(open,open)"}, ResourceOptions{}, "more than once"},
		{"Project", []string{"title:string", "status:enum(open,Closed)"}, ResourceOptions{}, "must be snake_case"},
		{"Project", []string{"title:string", "status:enum(open,closed"}, ResourceOptions{}, "close the values"},
		{"Project", []string{"title:string", "status:enum(open,closed)x"}, ResourceOptions{}, "unexpected"},
		{"Project", []string{"title:string", "x{{.Module}}:string"}, ResourceOptions{}, "snake_case"},
		{"9Lives", []string{"title:string"}, ResourceOptions{}, "must start with a letter"},
		{"x{{.Module}}", []string{"title:string"}, ResourceOptions{}, "must start with a letter"},
		{"Sheep", []string{"title:string"}, ResourceOptions{Plural: "Sheep"}, "must differ"},
		{"Default", []string{"title:string"}, ResourceOptions{Plural: "Default"}, "must differ"},
		{"Go", []string{"title:string"}, ResourceOptions{Plural: "Func"}, "Go package name"},
		{"User", []string{"title:string"}, ResourceOptions{Plural: "User"}, "must differ"},
		{"Row", []string{"title:string"}, ResourceOptions{Plural: "Table"}, "reserved in PostgreSQL"},
		{"Project", []string{"title:string"}, ResourceOptions{IDPrefix: "P"}, "ID prefix"},
		{"Changes", []string{"title:string"}, ResourceOptions{}, "declared twice"},
		{"Project", []string{"title:string", "state:enum(a_b,ab)"}, ResourceOptions{}, ""},
		{"Project", []string{"max:enum(name_length,other)", "name:string"}, ResourceOptions{}, "declared twice"},
		{"Project", []string{"title:string", "project_fields:string"}, ResourceOptions{}, "clashes"},
		{"Project", []string{"title:string", "org_id:string"}, ResourceOptions{}, "reserved"},
		{"Project", []string{"title:string", "created_by:string"}, ResourceOptions{}, "reserved"},
		{"Project", []string{"title:string"}, ResourceOptions{Scope: "global"}, "scope must be user or org"},
		{"Project", []string{"title:string"}, ResourceOptions{Scope: ScopeOrg}, ""},
	} {
		fields := title
		if tt.fields != nil || tt.wantErr == "at least one field" {
			fields, err = ParseFields(tt.fields)
		}
		if err == nil {
			_, err = NewResourceData("example.com/x", tt.name, fields, ResourceOptions{Plural: tt.opts.Plural, IDPrefix: tt.opts.IDPrefix, Scope: tt.opts.Scope, Migration: "20260915000000"})
		}
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s %v: error = %v", tt.name, tt.fields, err)
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s %v: error = %v, want containing %q", tt.name, tt.fields, err, tt.wantErr)
		}
	}
}
