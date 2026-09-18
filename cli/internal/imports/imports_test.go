package imports

import (
	"strings"
	"testing"
)

// toApp and toLibrary move the library's sign-in to an app's copy and back,
// as orb eject and go generate do.
func toApp(imp string) (string, string, bool) {
	switch {
	case imp == "gorbital.dev/gorbital/authhttp":
		return "example.com/shop/internal/modules/auth", "authhttp", true
	case strings.HasPrefix(imp, "gorbital.dev/gorbital/authhttp/internal/"):
		return "example.com/shop/internal/modules/auth/" + strings.TrimPrefix(imp, "gorbital.dev/gorbital/authhttp/internal/"), "", true
	}
	return "", "", false
}

func toLibrary(imp string) (string, string, bool) {
	switch {
	case imp == "example.com/shop/internal/modules/auth":
		return "gorbital.dev/gorbital/authhttp", "authhttp", true
	case strings.HasPrefix(imp, "example.com/shop/internal/modules/auth/"):
		return "gorbital.dev/gorbital/authhttp/internal/" + strings.TrimPrefix(imp, "example.com/shop/internal/modules/auth/"), "", true
	}
	return "", "", false
}

const library = `package main

import (
	"fmt"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/opshttp" // the operations API

	"example.com/shop/db/migrations"
	"example.com/shop/internal/modules"
)

func main() { fmt.Println(gorbital.Main, authhttp.New, opshttp.Module, migrations.FS, modules.All) }
`

const app = `package main

import (
	"fmt"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/opshttp" // the operations API

	"example.com/shop/db/migrations"
	"example.com/shop/internal/modules"
	authhttp "example.com/shop/internal/modules/auth"
)

func main() { fmt.Println(gorbital.Main, authhttp.New, opshttp.Module, migrations.FS, modules.All) }
`

// TestRewriteRoundTrip: an import moved to the app's copy gets the
// package's name and joins the app's own imports; moved back, it is the
// file it was.
func TestRewriteRoundTrip(t *testing.T) {
	got, changed, err := Rewrite("main.go", []byte(library), toApp)
	if err != nil || !changed || string(got) != app {
		t.Fatalf("Rewrite(toApp) = %v, %v\n%s\nwant:\n%s", changed, err, got, app)
	}
	back, changed, err := Rewrite("main.go", got, toLibrary)
	if err != nil || !changed || string(back) != library {
		t.Fatalf("Rewrite(toLibrary) = %v, %v\n%s\nwant:\n%s", changed, err, back, library)
	}
}

// TestRewriteKeepsGroupsItCantImprove: an import in a file with one group,
// or already among its kind, stays where it is; imports m doesn't map are
// untouched.
func TestRewriteKeepsGroupsItCantImprove(t *testing.T) {
	src := "package x\n\nimport (\n\t\"fmt\"\n\t\"gorbital.dev/gorbital/authhttp/internal/usecase\"\n)\n\nvar _ = fmt.Sprint(usecase.X)\n"
	want := "package x\n\nimport (\n\t\"example.com/shop/internal/modules/auth/usecase\"\n\t\"fmt\"\n)\n\nvar _ = fmt.Sprint(usecase.X)\n"
	if got, _, err := Rewrite("x.go", []byte(src), toApp); err != nil || string(got) != want {
		t.Errorf("Rewrite() = %v\n%s\nwant:\n%s", err, got, want)
	}
	plain := "package x\n\nimport \"fmt\"\n"
	if got, changed, err := Rewrite("x.go", []byte(plain), toApp); err != nil || changed || string(got) != plain {
		t.Errorf("Rewrite() without mapped imports = %q, %v, %v", got, changed, err)
	}
	single := "package x\n\nimport \"gorbital.dev/gorbital/authhttp\"\n\nvar _ = authhttp.New\n"
	wantSingle := "package x\n\nimport authhttp \"example.com/shop/internal/modules/auth\"\n\nvar _ = authhttp.New\n"
	if got, _, err := Rewrite("x.go", []byte(single), toApp); err != nil || string(got) != wantSingle {
		t.Errorf("Rewrite() of a single import = %v\n%s\nwant:\n%s", err, got, wantSingle)
	}
}

// TestRewriteMovesSeveral: two imports moved together each join their own
// kind, both ways.
func TestRewriteMovesSeveral(t *testing.T) {
	lib := "package x\n\nimport (\n\t\"gorbital.dev/gorbital\"\n\t\"gorbital.dev/gorbital/authhttp\"\n\t\"gorbital.dev/gorbital/orgshttp\"\n\n\t\"example.com/shop/internal/modules\"\n)\n\nvar _, _, _, _ = gorbital.Main, authhttp.New, orgshttp.Module, modules.All\n"
	own := "package x\n\nimport (\n\t\"gorbital.dev/gorbital\"\n\n\t\"example.com/shop/internal/modules\"\n\tauthhttp \"example.com/shop/internal/modules/auth\"\n\torgshttp \"example.com/shop/internal/modules/orgs\"\n)\n\nvar _, _, _, _ = gorbital.Main, authhttp.New, orgshttp.Module, modules.All\n"
	toOwn := func(imp string) (string, string, bool) {
		if name, ok := strings.CutPrefix(imp, "gorbital.dev/gorbital/"); ok && (name == "authhttp" || name == "orgshttp") {
			return "example.com/shop/internal/modules/" + strings.TrimSuffix(name, "http"), name, true
		}
		return "", "", false
	}
	toLib := func(imp string) (string, string, bool) {
		if dir, ok := strings.CutPrefix(imp, "example.com/shop/internal/modules/"); ok && (dir == "auth" || dir == "orgs") {
			return "gorbital.dev/gorbital/" + dir + "http", dir + "http", true
		}
		return "", "", false
	}
	if got, _, err := Rewrite("x.go", []byte(lib), toOwn); err != nil || string(got) != own {
		t.Errorf("Rewrite(toOwn) = %v\n%s\nwant:\n%s", err, got, own)
	}
	if got, _, err := Rewrite("x.go", []byte(own), toLib); err != nil || string(got) != lib {
		t.Errorf("Rewrite(toLib) = %v\n%s\nwant:\n%s", err, got, lib)
	}
}

// TestRewriteDotlessModule: goimports takes a module path without a dot,
// such as shop, for the standard library, so a copy imported by a file with
// no imports of the app's own joins the standard library's group, and goes
// back to the library's group.
func TestRewriteDotlessModule(t *testing.T) {
	lib := "package x_test\n\nimport (\n\t\"context\"\n\n\t\"gorbital.dev/config\"\n\t\"gorbital.dev/gorbital/authhttp\"\n)\n\nvar _, _, _ = context.Background, config.NewSecret, authhttp.New\n"
	own := "package x_test\n\nimport (\n\t\"context\"\n\tauthhttp \"shop/internal/modules/auth\"\n\n\t\"gorbital.dev/config\"\n)\n\nvar _, _, _ = context.Background, config.NewSecret, authhttp.New\n"
	toShop := func(imp string) (string, string, bool) {
		if imp == "gorbital.dev/gorbital/authhttp" {
			return "shop/internal/modules/auth", "authhttp", true
		}
		return "", "", false
	}
	fromShop := func(imp string) (string, string, bool) {
		if imp == "shop/internal/modules/auth" {
			return "gorbital.dev/gorbital/authhttp", "authhttp", true
		}
		return "", "", false
	}
	got, _, err := Rewrite("x_test.go", []byte(lib), toShop)
	if err != nil || string(got) != own {
		t.Fatalf("Rewrite(toShop) = %v\n%s\nwant:\n%s", err, got, own)
	}
	if back, _, err := Rewrite("x_test.go", got, fromShop); err != nil || string(back) != lib {
		t.Errorf("Rewrite(fromShop) = %v\n%s\nwant:\n%s", err, back, lib)
	}
}
