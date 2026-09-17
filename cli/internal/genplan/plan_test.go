package genplan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyWritesCreatesAndModifies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "internal/app/jobs.go", "before\n")
	plan := Plan{Generator: "job", Name: "Report", Changes: []Change{
		{Path: "internal/jobs/report/report.go", Kind: Create, Content: []byte("package report\n")},
		{Path: "internal/app/jobs.go", Kind: Modify, Before: []byte("before\n"), Content: []byte("after\n")},
	}}
	if err := Apply(dir, plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for path, want := range map[string]string{"internal/jobs/report/report.go": "package report\n", "internal/app/jobs.go": "after\n"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	if got := plan.Paths(); len(got) != 2 || got[0] != "internal/jobs/report/report.go" {
		t.Errorf("Paths() = %q", got)
	}
}

func TestCheckRefusesExistingAndStaleFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "x\n")
	writeFile(t, dir, "b.go", "edited\n")

	err := Check(dir, Plan{Changes: []Change{{Path: "a.go", Kind: Create, Content: []byte("y\n")}}})
	if !errors.Is(err, ErrExists) {
		t.Errorf("Check() with an existing file = %v, want ErrExists", err)
	}
	err = Check(dir, Plan{Changes: []Change{{Path: "b.go", Kind: Modify, Before: []byte("original\n"), Content: []byte("new\n")}}})
	if !errors.Is(err, ErrStale) {
		t.Errorf("Check() with a changed file = %v, want ErrStale", err)
	}
	if err := Apply(dir, Plan{Changes: []Change{{Path: "b.go", Kind: Modify, Before: []byte("original\n"), Content: []byte("new\n")}}}); !errors.Is(err, ErrStale) {
		t.Errorf("Apply() with a changed file = %v, want ErrStale", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.go")); string(got) != "edited\n" {
		t.Errorf("b.go = %q after a refused apply, want untouched", got)
	}
	if err := Check(dir, Plan{Changes: []Change{{Path: "c.go", Kind: "rename"}}}); err == nil {
		t.Error("Check() accepted an unknown kind")
	}
}

func TestApplyStaysInsideTheApp(t *testing.T) {
	dir := t.TempDir()
	err := Apply(dir, Plan{Changes: []Change{{Path: "../escape.go", Kind: Create, Content: []byte("x")}}})
	if err == nil {
		t.Fatal("Apply() wrote outside the app directory")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "..", "escape.go")); statErr == nil {
		t.Error("../escape.go exists")
	}
}

func TestChangeJSONRoundTrip(t *testing.T) {
	plan := Plan{Generator: "migration", Name: "add_phone", Changes: []Change{
		{Path: "db/migrations/1_add_phone.sql", Kind: Create, Content: []byte("-- +goose Up\n")},
		{Path: "internal/app/modules.go", Kind: Modify, Before: []byte("a"), Content: []byte("b")},
	}, Next: []string{"go run ./cmd/migrate"}}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var back Plan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Changes) != 2 || string(back.Changes[0].Content) != "-- +goose Up\n" || back.Changes[0].Before != nil ||
		string(back.Changes[1].Before) != "a" || string(back.Changes[1].Content) != "b" || back.Next[0] != "go run ./cmd/migrate" {
		t.Errorf("round trip = %+v", back)
	}
	if got := Describe(plan); got != "  create db/migrations/1_add_phone.sql\n  modify internal/app/modules.go\n" {
		t.Errorf("Describe() = %q", got)
	}
}

func TestDiff(t *testing.T) {
	before := "package modules\n\nimport (\n\t\"a\"\n\t\"b\"\n)\n\nfunc All() {\n\ta()\n\tb()\n}\n"
	after := "package modules\n\nimport (\n\t\"a\"\n\t\"b\"\n\t\"c\"\n)\n\nfunc All() {\n\ta()\n\tb()\n\tc()\n}\n"
	p := Plan{Changes: []Change{
		{Path: "x/new.go", Kind: Create, Content: []byte("package x\n")},
		{Path: "modules.gen.go", Kind: Modify, Before: []byte(before), Content: []byte(after)},
		{Path: "same.go", Kind: Modify, Before: []byte("a\n"), Content: []byte("a\n")},
	}}
	want := `--- /dev/null
+++ b/x/new.go
@@ -0,0 +1,1 @@
+package x
--- a/modules.gen.go
+++ b/modules.gen.go
@@ -3,9 +3,11 @@
 import (
 	"a"
 	"b"
+	"c"
 )
 
 func All() {
 	a()
 	b()
+	c()
 }
`
	if got := Diff(p); got != want {
		t.Errorf("Diff() =\n%s\nwant\n%s", got, want)
	}
}
