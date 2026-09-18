package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

// newEjected is a built-in module orb new copied into the app, in its JSON
// result.
type newEjected struct {
	Module string `json:"module"`
	// Package and Version are the library package it was copied from.
	Package   string `json:"package"`
	Version   string `json:"version"`
	Directory string `json:"directory"`
	// Files counts the module's files in Directory.
	Files int `json:"files"`
	// Migrations are its migrations, copied into db/migrations.
	Migrations []string `json:"migrations"`
}

// newAppLibrary returns the gorbital.dev/gorbital a new app builds with,
// without go list, which needs the go.sum go mod tidy writes: the
// directory of the --local checkout, or the version the templates require
// from the module cache, downloaded when it isn't there yet.
func newAppLibrary(ctx context.Context, dir, local string) (librarySource, error) {
	if local != "" {
		return librarySource{Version: recipes.LibraryVersion, Dir: filepath.Join(filepath.FromSlash(local), "gorbital")}, nil
	}
	var stderr bytes.Buffer
	module := gorbitalImportPath + "@" + recipes.LibraryVersion
	out, err := goOutputIn(ctx, dir, &stderr, "mod", "download", "-json", module)
	if err != nil {
		return librarySource{}, fmt.Errorf("download %s: %w: %s", module, err, strings.TrimSpace(stderr.String()))
	}
	var mod struct{ Dir, Error string }
	if err := json.Unmarshal(out, &mod); err != nil {
		return librarySource{}, fmt.Errorf("read go mod download's output: %w", err)
	}
	if mod.Dir == "" {
		return librarySource{}, fmt.Errorf("download %s: %s", module, cmpOr(mod.Error, "no source directory"))
	}
	return librarySource{Version: recipes.LibraryVersion, Dir: mod.Dir}, nil
}

// ejectIntoNewApp copies modules, in order, from lib into the app orb new
// has just written in dir, exactly as orb eject copies them: the package
// into internal/modules, its migrations into db/migrations, the app's
// imports changed to the copy, and an entry in gorbital.lock. orb eject's
// checks of an app someone may have changed, and the go commands they
// need, are left out: the templates hold what they look for, and
// TestNewAppIsTheGoldenApp proves the result.
func ejectIntoNewApp(dir string, modules []recipes.EjectedModule, lib librarySource, now time.Time) ([]newEjected, error) {
	app, err := findAppIn(dir)
	if err != nil {
		return nil, err
	}
	results := []newEjected{}
	for _, em := range modules {
		m, ok := lookupEjectable(em.Name)
		if !ok || m.importPath() != em.Package {
			return nil, fmt.Errorf("orb eject has no module %s copied from %s", em.Name, em.Package)
		}
		if err := checkModuleUse(dir, m); err != nil {
			return nil, fmt.Errorf("the template can't be ejected from: %w", err)
		}
		lock, err := readLock(dir)
		lockExists := err == nil
		if err != nil && !errors.Is(err, errNoLock) {
			return nil, err
		}
		plan, err := planEjectFrom(app, m, lib, lock, lockExists, now)
		if err != nil {
			return nil, err
		}
		if err := genplan.Apply(dir, plan); err != nil {
			return nil, err
		}
		res := plan.Result.(ejectResult)
		results = append(results, newEjected{
			Module: res.Module, Package: res.Package, Version: res.Version, Directory: res.Directory,
			Files: len(res.Files), Migrations: res.Migrations,
		})
	}
	return results, nil
}

// EjectIntoApp copies the built-in modules into the app in dir as orb new
// does after writing a Full app (preset.Ejects), from the gorbital checkout
// at checkout, and removes the gorbital.lock that records them when the app
// had none. go generate writes the golden apps with it, so they show what
// orb new writes; it runs neither go mod tidy nor the surface test.
func EjectIntoApp(dir string, modules []recipes.EjectedModule, checkout string) error {
	_, lockErr := readLock(dir)
	lib, err := newAppLibrary(context.Background(), dir, filepath.ToSlash(checkout))
	if err != nil {
		return err
	}
	if _, err := ejectIntoNewApp(dir, modules, lib, time.Now()); err != nil {
		return err
	}
	if errors.Is(lockErr, errNoLock) {
		return os.Remove(filepath.Join(dir, lockPath))
	}
	return nil
}

// ejectedAbout names what an ejected module is, for orb new's log.
func ejectedAbout(module string) string {
	if m, ok := lookupEjectable(module); ok {
		return m.about
	}
	return module
}
