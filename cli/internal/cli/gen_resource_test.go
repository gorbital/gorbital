package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testModulesGo = "package app\n\nimport \"errors\"\n\nfunc registerModules(api huma.API, mapper *httpx.Mapper, svc services) error {\n\treturn errors.Join(\n\t\t//orb:anchor modules\n\t\tregisterPing(api, mapper, svc.pingMessage),\n\t)\n}\n"

const testPermissionsGo = "package app\n\nfunc userResourcePermissions() []resourcePermissions {\n\treturn []resourcePermissions{\n\t\t//orb:anchor user-permissions\n\t}\n}\n"

// newResourceApp creates a minimal stand-in for a Full preset app with the
// auth module and makes it the working directory.
func newResourceApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "internal", "app", "modules.go"), testModulesGo)
	writeFile(t, filepath.Join(dir, "internal", "app", "permissions.go"), testPermissionsGo)
	writeFile(t, filepath.Join(dir, "internal", "modules", "auth", "module.go"), "package auth\n")
	writeFile(t, filepath.Join(dir, "db", "migrations", "20260915000001_auth.sql"), "-- auth\n")
	t.Chdir(dir)
	return dir
}

func TestGenResourceWithFields(t *testing.T) {
	newResourceApp(t)
	code, out, errOut := runOrb(t, "gen", "resource", "Project", "name:string:unique", "description:text", "status:enum(active,archived)", "--json")
	if code != 0 {
		t.Fatalf("orb gen resource = %d, stderr %q", code, errOut)
	}
	var res genResourceResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Module != "projects" || res.Route != "/v1/projects" || res.Table != "projects" ||
		len(res.Files) != 22 || res.DryRun {
		t.Fatalf("orb gen resource --json = %q (%v)", out, err)
	}
	for _, f := range res.Files {
		if _, err := os.Stat(filepath.FromSlash(f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
		if dir, name, ok := strings.Cut(f, "db/migrations/"); ok && dir == "" {
			if version, _, _ := strings.Cut(name, "_"); version <= "20260915000001" || !strings.HasSuffix(name, "_projects.sql") {
				t.Errorf("migration %s doesn't run after the existing one", name)
			}
		}
	}

	for path, want := range map[string]string{
		"internal/modules/projects/domain/project.go": "type ProjectFields struct",
		"internal/modules/projects/domain/errors.go":  "ErrProjectNameTaken",
		"internal/app/module_projects.go":             `"example.com/shop/internal/modules/projects"`,
		"internal/app/modules.go":                     "//orb:anchor modules\n\t\tregisterProjects(api, mapper, svc),\n\t\tregisterPing(",
		"internal/app/permissions.go":                 "//orb:anchor user-permissions\n\t\tprojectsPermissions,\n",
	} {
		if got := readFile(t, filepath.FromSlash(path)); !strings.Contains(got, want) {
			t.Errorf("%s lacks %q:\n%s", path, want, got)
		}
	}

	if code, _, errOut := runOrb(t, "gen", "resource", "Project", "name:string"); code != 1 || !strings.Contains(errOut, "already registered") {
		t.Errorf("generating the same resource twice = %d %q, want already registered", code, errOut)
	}
}

func TestGenResourceFlagsAnywhereAndDryRun(t *testing.T) {
	dir := newResourceApp(t)
	code, out, errOut := runOrb(t, "gen", "resource", "--dry-run", "Person", "name:string", "--plural", "People", "--id-prefix", "per", "bio:text")
	if code != 0 || !strings.Contains(out, "Would create (dry run)") || !strings.Contains(out, "internal/modules/people/module.go") ||
		!strings.Contains(out, "IDs like per_") || !strings.Contains(out, "bio") {
		t.Fatalf("orb gen resource --dry-run = %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "modules", "people")); !os.IsNotExist(err) {
		t.Error("dry run wrote the module")
	}
	if got := readFile(t, filepath.Join(dir, "internal", "app", "modules.go")); got != testModulesGo {
		t.Errorf("dry run changed modules.go:\n%s", got)
	}
}

func TestGenResourceValidation(t *testing.T) {
	newResourceApp(t)
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"missing name", []string{"gen", "resource", "--yes"}, 2, "missing resource name"},
		{"missing fields", []string{"gen", "resource", "Project"}, 2, "missing fields"},
		{"unknown type", []string{"gen", "resource", "Project", "name:number"}, 2, "type must be string, text or enum"},
		{"no string field", []string{"gen", "resource", "Project", "notes:text"}, 2, "at least one string field"},
		{"template injection", []string{"gen", "resource", "x{{.Module}}", "name:string"}, 2, "must start with a letter"},
		{"field injection", []string{"gen", "resource", "Project", "x`+os.Exit(1)+`:string"}, 2, "snake_case"},
		{"org scope without organisations", []string{"gen", "resource", "Project", "name:string", "--scope", "org"}, 1, "--scope org needs organisations"},
		{"unknown scope", []string{"gen", "resource", "Project", "name:string", "--scope", "global"}, 2, "unknown --scope"},
		{"same plural", []string{"gen", "resource", "Sheep", "name:string", "--plural", "Sheep"}, 2, "must differ"},
		{"bad ID prefix", []string{"gen", "resource", "Project", "name:string", "--id-prefix", "P1"}, 2, "ID prefix"},
		{"reserved field", []string{"gen", "resource", "Project", "version:string"}, 2, "reserved"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runOrb(t, tt.args...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("orb %s = %d %q, want %d containing %q", strings.Join(tt.args, " "), code, errOut, tt.wantCode, tt.wantErr)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join("internal", "modules")); len(entries) != 1 {
		t.Errorf("failed commands wrote modules: %v", entries)
	}
}

func TestGenResourceOutsideFullPresetApp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/minimal\n")
	t.Chdir(dir)
	args := []string{"gen", "resource", "Project", "name:string"}
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "Full preset") {
		t.Errorf("orb gen resource in a Minimal app = %d %q, want Full preset guidance", code, errOut)
	}

	writeFile(t, filepath.Join(dir, "internal", "app", "modules.go"), testModulesGo)
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "internal/modules/auth") {
		t.Errorf("orb gen resource without the auth module = %d %q", code, errOut)
	}

	writeFile(t, filepath.Join(dir, "internal", "modules", "auth", "module.go"), "package auth\n")
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "db/migrations") {
		t.Errorf("orb gen resource without db/migrations = %d %q", code, errOut)
	}

	writeFile(t, filepath.Join(dir, "db", "migrations", "20260915000001_auth.sql"), "")
	writeFile(t, filepath.Join(dir, "internal", "app", "modules.go"), "package app\n\nfunc registerModules() error { return nil }\n")
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "//orb:anchor modules") {
		t.Errorf("orb gen resource without the anchor = %d %q, want the line to add", code, errOut)
	}

	oldStyle := "package app\n\nfunc registerModules() error {\n\t//orb:anchor modules\n\tif err := registerPing(); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n"
	writeFile(t, filepath.Join(dir, "internal", "app", "modules.go"), oldStyle)
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "errors.Join") {
		t.Errorf("orb gen resource with statement-style modules.go = %d %q, want errors.Join guidance", code, errOut)
	}

	writeFile(t, filepath.Join(dir, "internal", "app", "modules.go"), testModulesGo)
	writeFile(t, filepath.Join(dir, "internal", "app", "permissions.go"), "package app\n")
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "//orb:anchor user-permissions") {
		t.Errorf("orb gen resource without the user permissions anchor = %d %q, want the line to add", code, errOut)
	}

	writeFile(t, filepath.Join(dir, "internal", "app", "permissions.go"), testPermissionsGo)
	writeFile(t, filepath.Join(dir, "internal", "modules", "projects", "keep.go"), "package projects\n")
	if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "internal/modules/projects already exists") {
		t.Errorf("orb gen resource over an existing module = %d %q", code, errOut)
	}
}

func TestNextMigrationVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db", "migrations", "20260915000001_auth.sql"), "")
	writeFile(t, filepath.Join(dir, "db", "migrations", "migrations.go"), "package migrations\n")
	now := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	if got, err := nextMigrationVersion(dir, now); err != nil || got != "20260916083000" {
		t.Errorf("nextMigrationVersion() = %q, %v, want now", got, err)
	}
	writeFile(t, filepath.Join(dir, "db", "migrations", "20260916083000_same_second.sql"), "")
	if got, err := nextMigrationVersion(dir, now); err != nil || got != "20260916083001" {
		t.Errorf("nextMigrationVersion() with a migration at now = %q, %v, want one later", got, err)
	}
}
