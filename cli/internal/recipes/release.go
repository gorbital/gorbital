package recipes

import (
	"fmt"
	"io/fs"
)

// A Release is the templates of one orb release, laid out like this
// package's directory: minimal/, full/, full-multi/ and mail/, with
// v0.2/full/ and v0.2/full-multi/ in v0.2 releases and v0.3/ from
// v0.3.0 on. Every release uses the same template format, and keeps each
// directory for the layout it has always held (ADR-0050).
type Release struct {
	fsys fs.FS
}

// Embedded returns this orb's own templates.
func Embedded() Release { return Release{fsys: templatesFS} }

// ReleaseFS returns the templates of a release whose cli/internal/recipes
// directory is fsys, such as an older release fetched by orb upgrade. They
// are read and rendered as text; nothing in them is compiled or run.
func ReleaseFS(fsys fs.FS) Release { return Release{fsys: fsys} }

// Tree renders every file orb writes for an app of preset and tenancy in
// layout (LayoutV01 or LayoutV02) that sends email with mail ("" for
// presets without email), keyed by slash-separated path: the preset's
// templates, then the provider's files, exactly as orb new and orb add mail
// write them.
func (r Release) Tree(preset, tenancy, layout, mail string, d Data) (map[string][]byte, error) {
	tree, err := r.appTree(preset, tenancy, layout, d)
	if err != nil {
		return nil, err
	}
	if mail == "" {
		return tree, nil
	}

	_, infra := tree[InfraMailPath]
	_, main := tree[MainMailPath]
	if !infra && !main {
		return nil, fmt.Errorf("recipes: the %s preset has no email provider to set to %s", preset, mail)
	}
	m, err := r.Mail(mail, d.Module)
	if err != nil {
		return nil, err
	}
	if infra {
		tree[InfraMailPath] = m.InfraMail
		// Releases before the provider's tests moved into their own file have
		// none; orb add mail writes it only where the tree has it.
		if _, ok := tree[InfraMailTestPath]; ok {
			tree[InfraMailTestPath] = m.InfraMailTest
		}
	}
	if main {
		if m.MainMail == nil {
			return nil, fmt.Errorf("recipes: this release has no %s for %s", MainMailPath, mail)
		}
		tree[MainMailPath] = m.MainMail
	}
	if tree[envExamplePath], err = ReplaceBlock(tree[envExamplePath], MailBlock, m.EnvBlock); err != nil {
		return nil, fmt.Errorf("recipes: %s: %w", envExamplePath, err)
	}
	if manifest, ok := tree[manifestPath]; ok {
		tree[manifestPath] = SetManifestKey(manifest, "mail", mail)
	}
	return tree, nil
}

// appTree renders the preset's or the profile's templates. Since v0.3.0
// the v0.2 layout has one tree and a profile chooses what it writes; an
// older release fetched by orb upgrade still has v0.2/full and
// v0.2/full-multi, and is rendered from those.
func (r Release) appTree(preset, tenancy, layout string, d Data) (map[string][]byte, error) {
	if known, ok := LookupPreset(preset, TenancySingle); layout == LayoutV02 && ok && known.Layout() == LayoutV02 {
		if _, err := fs.Stat(r.fsys, TreeV03); err == nil {
			p := d.Profile
			if p.Auth == "" {
				if p, err = ProfileFromTenancy(tenancy); err != nil {
					return nil, fmt.Errorf("recipes: %w", err)
				}
			}
			return RenderTree(r.fsys, TreeV03, p, d)
		}
	}
	p, ok := LookupPreset(preset, tenancy)
	if !ok {
		return nil, fmt.Errorf("recipes: no %s preset with %s tenancy", preset, tenancy)
	}
	dir, err := p.treeDir(layout)
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(r.fsys, dir); err != nil {
		return nil, fmt.Errorf("recipes: this release has no templates for the %s preset with %s tenancy in the %s layout: %w", preset, tenancy, layout, err)
	}
	return renderTree(r.fsys, dir, d)
}

const (
	envExamplePath = ".env.example"
	manifestPath   = "gorbital.yaml"
)
