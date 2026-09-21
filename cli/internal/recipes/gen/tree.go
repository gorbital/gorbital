package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gorbital.dev/cli/internal/recipes"
	"gorbital.dev/cli/internal/recipes/generate"
)

// treeFS holds the hand-written part of the v0.2.2 tree: manifest.yaml,
// the conditional templates (ADR-0090 §6) and the templates no golden app
// has, such as a custom scope's stub. generate.Run writes the rest from
// examples/full-multi and these are copied over it, so one directory holds
// everything a reader has to read to know what a profile gets.
//
//go:embed all:tree
var treeFS embed.FS

// generatedPrefixes are the golden app's directories orb new produces
// rather than writes from templates, so they are no part of the tree
// (ADR-0090 §5): the demonstration module, which orb gen module writes at
// the profile's scope, and the API artefacts, which the app's own openapi
// command writes.
var generatedPrefixes = []string{"internal/modules/projects/", "api/"}

// templated reports whether the golden app's file at rel becomes a
// template.
func templated(rel string) bool {
	for _, prefix := range generatedPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return false
		}
	}
	return !strings.HasSuffix(rel, "_projects.sql")
}

// A patch is one edit conditionalise makes to a generated template: from
// must appear exactly once, and becomes to.
type patch struct{ from, to string }

// patches are the conditional edits to templates generate.Run writes,
// where replacing the whole file by hand would mean maintaining text the
// golden app already has. Everything else conditional is a hand-written
// template under tree/.
var patches = map[string][]patch{
	// The demonstration module is generated after the templates are
	// rendered (orb gen module), and adds itself to this file.
	"internal/modules/modules.gen.go.tmpl": {
		{from: "\t\"⟦.Module⟧/internal/modules/ping\"\n\t\"⟦.Module⟧/internal/modules/projects\"\n", to: "\t\"⟦.Module⟧/internal/modules/ping\"\n"},
		{from: "\t\tping.Module(),\n\t\tprojects.Module(),\n", to: "\t\tping.Module(),\n"},
	},
	// Organisations are a requirement only of an app that mounts them.
	"go.mod.tmpl": {
		{
			from: "\tgorbital.dev/modules/orgs ⟦.LibraryVersion⟧ // indirect\n",
			to:   "⟦- if .Profile.Named⟧\n\tgorbital.dev/modules/orgs ⟦.LibraryVersion⟧ // indirect\n⟦- end⟧\n",
		},
		{
			from: "\tgorbital.dev/modules/orgs => ⟦.LocalDir \"/modules/orgs\"⟧\n",
			to:   "⟦- if .Profile.Named⟧\n\tgorbital.dev/modules/orgs => ⟦.LocalDir \"/modules/orgs\"⟧\n⟦- end⟧\n",
		},
	},
}

// conditionalise applies the patches to the tree at dst.
func conditionalise(dst string) error {
	for rel, edits := range patches {
		name := filepath.Join(dst, filepath.FromSlash(rel))
		src, err := os.ReadFile(name) //nolint:gosec // a template gen just wrote
		if err != nil {
			return fmt.Errorf("conditionalise %s: %w", rel, err)
		}
		text := string(src)
		for _, e := range edits {
			if n := strings.Count(text, e.from); n != 1 {
				return fmt.Errorf("conditionalise %s: the text to make conditional appears %d times, want once:\n%s", rel, n, e.from)
			}
			text = strings.Replace(text, e.from, e.to, 1)
		}
		if err := os.WriteFile(name, []byte(text), 0o644); err != nil { //nolint:gosec // a template
			return err
		}
	}
	return nil
}

// copyTreeExtras writes the hand-written part of the tree over the
// generated one.
func copyTreeExtras(dst string) error {
	return fs.WalkDir(treeFS, "tree", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "tree/")
		content, err := treeFS.ReadFile(p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644) //nolint:gosec // a template
	})
}

// checkManifest fails when a template in the tree has no manifest entry,
// or an entry names a template the tree lacks.
func checkManifest(dst string) error {
	return recipes.CheckManifest(os.DirFS(filepath.Dir(dst)), filepath.Base(dst))
}

// renderInto writes the app the profile asks for, from the tree at dir,
// into the app at app: a golden app is what the templates produce, not the
// other way round, so a conditional template and the app it writes can't
// drift apart. go.mod is left alone — the golden apps replace the library
// with this checkout, and generate.GoModTemplate already derives the
// template from theirs.
func renderInto(dir, app string, profile recipes.Profile) error {
	d := recipes.Data{
		Name:           generate.PlaceholderName,
		Module:         generate.PlaceholderModule,
		LibraryVersion: recipes.LibraryVersion,
		Profile:        profile,
	}
	tree, err := recipes.RenderTree(os.DirFS(filepath.Dir(dir)), filepath.Base(dir), profile, d)
	if err != nil {
		return fmt.Errorf("render %s into %s: %w", dir, app, err)
	}
	if len(tree) == 0 {
		return fmt.Errorf("render %s into %s: no files", dir, app)
	}
	// orb new records the demonstration module's access rule in
	// gorbital.yaml as orb gen module does, so the golden app has it too.
	if manifest, ok := tree[manifestPath]; ok {
		tree[manifestPath] = recipes.SetModuleScope(manifest, demoPackage, profile.ResourceScope())
	}
	for rel, content := range tree {
		if renderSkipped[rel] {
			continue
		}
		target := filepath.Join(app, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil { //nolint:gosec // an app gen owns
			return err
		}
	}
	return nil
}

// renderSkipped are the files of a golden app the render-back leaves
// alone: go.mod, whose replace directives point at this checkout and
// whose template is derived from it, and modules.gen.go, which the
// demonstration module orb new generates adds itself to.
var renderSkipped = map[string]bool{"go.mod": true, "internal/modules/modules.gen.go": true}

// manifestPath is the app's gorbital.yaml, and demoPackage the Go package
// of the demonstration module orb new generates.
const (
	manifestPath = "gorbital.yaml"
	demoPackage  = "projects"
)

// produceAPI writes the app's API artefacts as orb new does after go mod
// tidy (ADR-0090 §5): the OpenAPI document, the Postman collection and
// llms.txt from the app's own command, and the /ops baseline the app is
// held to from the document it was created with.
func produceAPI(ctx context.Context, dir string) error {
	if err := goIn(ctx, dir, "run", "./cmd/api", "openapi", "--dir", "api"); err != nil {
		return err
	}
	document, err := os.ReadFile(filepath.Join(dir, "api", "openapi.json")) //nolint:gosec // the app's own output
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "api", "openapi.baseline.json"), document, 0o644) //nolint:gosec // the app's own output
}
