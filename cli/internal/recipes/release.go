package recipes

import (
	"fmt"
	"io/fs"
)

// A Release is the templates of one aps release, laid out like this
// package's directory: minimal/, full/, full-multi/ and mail/. Every release
// since v0.2.0 uses the same layout and template format (ADR-0050).
type Release struct {
	fsys fs.FS
}

// Embedded returns this aps's own templates.
func Embedded() Release { return Release{fsys: templatesFS} }

// ReleaseFS returns the templates of a release whose cli/internal/recipes
// directory is fsys, such as an older release fetched by aps upgrade. They
// are read and rendered as text; nothing in them is compiled or run.
func ReleaseFS(fsys fs.FS) Release { return Release{fsys: fsys} }

// Tree renders every file aps writes for an app of preset and tenancy that
// sends email with mail ("" for presets without email), keyed by
// slash-separated path: the preset's templates, then the provider's files,
// exactly as aps new and aps add mail write them.
func (r Release) Tree(preset, tenancy, mail string, d Data) (map[string][]byte, error) {
	p, ok := LookupPreset(preset, tenancy)
	if !ok {
		return nil, fmt.Errorf("recipes: no %s preset with %s tenancy", preset, tenancy)
	}
	if _, err := fs.Stat(r.fsys, p.dir); err != nil {
		return nil, fmt.Errorf("recipes: this release has no templates for the %s preset with %s tenancy: %w", preset, tenancy, err)
	}
	tree, err := renderTree(r.fsys, p.dir, d)
	if err != nil {
		return nil, err
	}
	if mail == "" {
		return tree, nil
	}

	if _, ok := tree[InfraMailPath]; !ok {
		return nil, fmt.Errorf("recipes: the %s preset has no email provider to set to %s", preset, mail)
	}
	m, err := r.Mail(mail, d.Module)
	if err != nil {
		return nil, err
	}
	tree[InfraMailPath] = m.InfraMail
	// Releases before the provider's tests moved into their own file have
	// none; aps add mail writes it only where the tree has it.
	if _, ok := tree[InfraMailTestPath]; ok {
		tree[InfraMailTestPath] = m.InfraMailTest
	}
	if tree[envExamplePath], err = ReplaceBlock(tree[envExamplePath], MailBlock, m.EnvBlock); err != nil {
		return nil, fmt.Errorf("recipes: %s: %w", envExamplePath, err)
	}
	if manifest, ok := tree[manifestPath]; ok {
		tree[manifestPath] = SetManifestKey(manifest, "mail", mail)
	}
	return tree, nil
}

const (
	envExamplePath = ".env.example"
	manifestPath   = "apistock.yaml"
)
