package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDoctorOnAMainApp(t *testing.T) {
	dir := newMainApp(t, true)
	writeFile(t, ".env", readFile(t, ".env.example"))
	calls := fakeDoctorCommands(t, `{"current":20260920000001,"latest":20260920000001,"pending":0}`)

	res := doctorRun(t, 0)
	if res.Layout != layoutMain {
		t.Errorf("layout = %q, want main", res.Layout)
	}
	for name, want := range map[string]string{
		"modules":  "internal/modules/modules.gen.go lists 1 module: books",
		"stack":    "the default middleware stack",
		"timeout":  "30s, the default",
		"database": "at migration 20260920000001, none pending",
	} {
		if c := check(res, name); c.Status != doctorOK || !strings.Contains(c.Detail, want) {
			t.Errorf("check %s = %+v, want ok %q", name, c, want)
		}
	}
	for _, c := range res.Checks {
		if c.Name == "anchor" || c.Name == "anchors" {
			t.Errorf("a v0.1 check ran in an app on gorbital.Main: %+v", c)
		}
	}
	if !slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "go run ./cmd/api migrate --status --json") }) {
		t.Errorf("the migration status wasn't read through ./cmd/api migrate: %q", *calls)
	}

	// Pending migrations of the merged history name the app's command.
	fakeDoctorCommands(t, `{"current":20260914000001,"latest":20260920000001,"pending":3}`)
	if c := check(doctorRun(t, 0), "database"); c.Status != doctorWarn || c.Fix != "go run ./cmd/api migrate (orb dev runs them)" {
		t.Errorf("pending migrations = %+v", c)
	}
	fakeDoctorCommands(t, `{"current":20990101000000,"latest":20260920000001,"pending":0}`)
	if c := check(doctorRun(t, 1), "database"); c.Status != doctorFail || !strings.Contains(c.Detail, "the app and the library") {
		t.Errorf("database ahead of the code = %+v", c)
	}
	_ = dir
}

func TestDoctorFindsMainAppProblems(t *testing.T) {
	dir := newMainApp(t, true)
	writeFile(t, ".env", readFile(t, ".env.example")+"\nAPP_REQUEST_TIMEOUT=2m\n")
	// A module modules.gen.go doesn't list, a directory that isn't a module,
	// and a custom stack without Auth.
	writeFile(t, filepath.Join(dir, "internal", "modules", "reviews", "module.go"), "package reviews\n\nimport \"gorbital.dev/gorbital\"\n\nfunc Module() gorbital.Module { return gorbital.Module{Name: \"reviews\"} }\n")
	writeFile(t, filepath.Join(dir, "internal", "modules", "shared", "util.go"), "package shared\n")
	writeFile(t, filepath.Join(dir, "cmd", "api", "stack.go"), `package main

import (
	"net/http"

	"gorbital.dev/gorbital"
)

var stack = gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{s.Recover, s.RequestID}
})
`)
	fakeDoctorCommands(t, `{"current":1,"latest":1,"pending":0}`)
	res := doctorRun(t, 1, "--fast")
	var modules []doctorCheck
	for _, c := range res.Checks {
		if c.Name == "modules" {
			modules = append(modules, c)
		}
	}
	if len(modules) != 2 || modules[0].Status != doctorWarn || !strings.Contains(modules[0].Detail, "internal/modules/shared has Go files but no func Module()") ||
		modules[1].Status != doctorFail || !strings.Contains(modules[1].Detail, "is stale: reviews isn't listed") || modules[1].Fix != "orb gen modules (orb dev runs it before each build)" {
		t.Errorf("modules checks = %+v", modules)
	}
	if c := check(res, "stack"); c.Status != doctorWarn || !strings.Contains(c.Detail, "cmd/api/stack.go:9 leaves out Auth (no request is authenticated") || strings.Contains(c.Detail, "Recover") {
		t.Errorf("stack = %+v", c)
	}
	if c := check(res, "timeout"); c.Status != doctorFail || !strings.Contains(c.Detail, "APP_REQUEST_TIMEOUT=2m0s isn't shorter than the server's 1m0s write timeout") {
		t.Errorf("timeout = %+v", c)
	}

	// modules.gen.go removed; a stack from a named function using Default;
	// the timeout turned off.
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(modulesGenPath))); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "cmd", "api", "stack.go"), `package main

import (
	"net/http"

	g "gorbital.dev/gorbital"
)

func order(st g.Stack) []func(http.Handler) http.Handler { return append(st.Default(), http.StripPrefix("/api", nil)) }

var stack = g.WithStack(order)

var opaque = g.WithStack(stackFrom(1))
`)
	writeFile(t, ".env", readFile(t, ".env.example")+"\nAPP_REQUEST_TIMEOUT=0\n")
	res = doctorRun(t, 1, "--fast")
	if !slices.ContainsFunc(res.Checks, func(c doctorCheck) bool {
		return c.Name == "modules" && c.Status == doctorFail && strings.Contains(c.Detail, "modules.gen.go is missing")
	}) {
		t.Errorf("missing modules.gen.go not reported: %+v", res.Checks)
	}
	var stacks []doctorCheck
	for _, c := range res.Checks {
		if c.Name == "stack" {
			stacks = append(stacks, c)
		}
	}
	if len(stacks) != 2 || stacks[0].Status != doctorOK || !strings.Contains(stacks[0].Detail, "builds on the default one") ||
		stacks[1].Status != doctorWarn || !strings.Contains(stacks[1].Detail, "can't be checked") {
		t.Errorf("stack checks = %+v", stacks)
	}
	if c := check(res, "timeout"); c.Status != doctorWarn || !strings.Contains(c.Detail, "turns the timeout off") {
		t.Errorf("timeout 0 = %+v", c)
	}
	writeFile(t, ".env", readFile(t, ".env.example")+"\nAPP_REQUEST_TIMEOUT=soon\n")
	if c := check(doctorRun(t, 1, "--fast"), "timeout"); c.Status != doctorFail || !strings.Contains(c.Detail, "isn't a duration") {
		t.Errorf("timeout soon = %+v", c)
	}
}
