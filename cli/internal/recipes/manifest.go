package recipes

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
)

// TreeV022 is the one template tree orb new writes every profile from
// (ADR-0090 §3). It replaced v0.2/full and v0.2/full-multi: the tree is
// the union of every shape, and manifest.yaml is the only mapping from a
// feature to the files it owns.
const TreeV022 = "v0.2.2"

// ManifestName is the file in a template tree that says which features
// each path needs.
const ManifestName = "manifest.yaml"

// The features a manifest entry can require. A path is written when every
// feature it lists is on, and skipped otherwise.
const (
	// FeatureAuthLocal: the app has gorbital's sign-in (--auth basic or
	// --auth full).
	FeatureAuthLocal = "auth.local"
	// FeatureAuthFull: the app serves every sign-in method (--auth full).
	FeatureAuthFull = "auth.full"
	// FeatureScopeNamed: the app mounts the supplied organisations under
	// its own vocabulary (--scope <name>).
	FeatureScopeNamed = "scope.named"
	// FeatureScopeCustom: the app writes its own membership rules
	// (--scope custom).
	FeatureScopeCustom = "scope.custom"
	// FeatureScopeAny: the app has a tenancy of either kind.
	FeatureScopeAny = "scope.any"
)

// features are the feature names a manifest may use, so a typo in one is
// an error rather than a path nothing ever writes.
var features = []string{FeatureAuthLocal, FeatureAuthFull, FeatureScopeNamed, FeatureScopeCustom, FeatureScopeAny}

// alwaysValue is the manifest value of a path every profile gets.
const alwaysValue = "always"

// Features returns the features the profile turns on.
func (p Profile) Features() map[string]bool {
	return map[string]bool{
		FeatureAuthLocal:   p.SignsIn(),
		FeatureAuthFull:    p.Auth == AuthFull,
		FeatureScopeNamed:  p.Named(),
		FeatureScopeCustom: p.CustomScope(),
		FeatureScopeAny:    p.Scoped(),
	}
}

// A manifest maps each template path in a tree to the features that must
// be on for it to be written. A path with no entry is an error, and an
// entry naming a path the tree lacks is the same error the other way
// round: both fail go generate and CI, never a user's orb new.
type manifest map[string][]string

// parseManifest reads a tree's manifest.yaml. It is read strictly, unlike
// an app's gorbital.yaml: this file is ours, and a mistake in it must stop
// the build rather than quietly drop a file from somebody's app.
func parseManifest(src []byte) (manifest, error) {
	m := manifest{}
	inPaths := false
	for i, line := range strings.Split(string(src), "\n") {
		text := strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(text, " ") && !strings.HasPrefix(text, "\t") {
			key, rest, ok := strings.Cut(trimmed, ":")
			if !ok || strings.TrimSpace(rest) != "" {
				return nil, fmt.Errorf("%s line %d: expected a top-level block, got %q", ManifestName, i+1, trimmed)
			}
			inPaths = key == "paths"
			continue
		}
		if !inPaths {
			continue
		}
		path, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("%s line %d: expected <path>: <features>, got %q", ManifestName, i+1, trimmed)
		}
		path = strings.TrimSpace(path)
		value = strings.TrimSpace(value)
		if _, seen := m[path]; seen {
			return nil, fmt.Errorf("%s line %d: %s is listed twice", ManifestName, i+1, path)
		}
		needed, err := parseFeatures(value)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %s: %w", ManifestName, i+1, path, err)
		}
		m[path] = needed
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("%s has no paths: block", ManifestName)
	}
	return m, nil
}

// parseFeatures reads a manifest value: always, or a flow sequence of
// feature names such as [scope.named].
func parseFeatures(value string) ([]string, error) {
	if value == alwaysValue {
		return nil, nil
	}
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected %s or a list such as [%s], got %q", alwaysValue, FeatureScopeNamed, value)
	}
	var needed []string
	for _, name := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !slices.Contains(features, name) {
			return nil, fmt.Errorf("unknown feature %q (want %s)", name, strings.Join(features, ", "))
		}
		needed = append(needed, name)
	}
	if len(needed) == 0 {
		return nil, fmt.Errorf("an empty list writes the path for no profile; use %s or name a feature", alwaysValue)
	}
	return needed, nil
}

// Wants reports whether an app with these features gets a path needing
// them, for orb doctor.
func Wants(on map[string]bool, needed []string) bool { return wants(on, needed) }

// wants reports whether an app with these features gets the path.
func wants(on map[string]bool, needed []string) bool {
	for _, f := range needed {
		if !on[f] {
			return false
		}
	}
	return true
}

// Render writes the profile's app into root and returns the files written,
// sorted by path. The demonstration module and the API artefacts are not
// among them: orb new generates the one and produces the other
// (ADR-0090 §5, §6).
func (p Profile) Render(root *os.Root, d Data) ([]File, error) {
	d.Profile = p
	tree, err := RenderTree(templatesFS, TreeV022, p, d)
	if err != nil {
		return nil, err
	}
	return writeTree(root, tree)
}

// RenderTree renders the templates under base in fsys that the profile
// asks for, keyed by the slash-separated path of each file they produce.
// It is Render without the writing, for the generator and the tests.
func RenderTree(fsys fs.FS, base string, p Profile, d Data) (map[string][]byte, error) {
	d.Profile = p
	return renderProfileTree(fsys, base, p, d)
}

// renderProfileTree renders the templates under base that the profile's
// features ask for. It checks the manifest against the tree both ways: a
// template with no entry, or an entry with no template, is an error naming
// the path.
func renderProfileTree(fsys fs.FS, base string, p Profile, d Data) (map[string][]byte, error) {
	src, err := fs.ReadFile(fsys, base+"/"+ManifestName)
	if err != nil {
		return nil, fmt.Errorf("recipes: %s: %w", base+"/"+ManifestName, err)
	}
	m, err := parseManifest(src)
	if err != nil {
		return nil, fmt.Errorf("recipes: %s/%w", base, err)
	}
	if err := CheckManifest(fsys, base); err != nil {
		return nil, err
	}
	on := p.Features()
	keep := map[string]bool{}
	for path, needed := range m {
		if wants(on, needed) {
			keep[path] = true
		}
	}
	return renderTreeFunc(fsys, base, d, func(rel string) bool {
		return rel != ManifestName && keep[rel]
	})
}

// CheckManifest reports a template in the tree at base with no manifest
// entry, and a manifest entry naming a template the tree lacks. Both fail
// go generate and CI, so a template added without an entry can't silently
// stop being written for some profile.
func CheckManifest(fsys fs.FS, base string) error {
	src, err := fs.ReadFile(fsys, base+"/"+ManifestName)
	if err != nil {
		return fmt.Errorf("recipes: %s: %w", base+"/"+ManifestName, err)
	}
	m, err := parseManifest(src)
	if err != nil {
		return fmt.Errorf("recipes: %s/%w", base, err)
	}
	listed := map[string]bool{}
	var missing []string
	err = fs.WalkDir(fsys, base, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, base+"/")
		if rel == ManifestName {
			return nil
		}
		if _, ok := m[rel]; !ok {
			missing = append(missing, rel)
			return nil
		}
		listed[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	var extra []string
	for rel := range m {
		if !listed[rel] {
			extra = append(extra, rel)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	switch {
	case len(missing) > 0 && len(extra) > 0:
		return fmt.Errorf("recipes: %s/%s: %s have no entry, and %s name no template; every template needs an entry saying which features it belongs to",
			base, ManifestName, strings.Join(missing, ", "), strings.Join(extra, ", "))
	case len(missing) > 0:
		return fmt.Errorf("recipes: %s/%s has no entry for %s; add one saying which features the template belongs to (%s for every profile)",
			base, ManifestName, strings.Join(missing, ", "), alwaysValue)
	case len(extra) > 0:
		return fmt.Errorf("recipes: %s/%s names %s, which the tree doesn't have; remove the entry or add the template",
			base, ManifestName, strings.Join(extra, ", "))
	}
	return nil
}

// ManifestPaths returns the tree's manifest entries, for the tests and orb
// doctor: each template path with the features it needs.
func ManifestPaths(fsys fs.FS, base string) (map[string][]string, error) {
	src, err := fs.ReadFile(fsys, base+"/"+ManifestName)
	if err != nil {
		return nil, err
	}
	m, err := parseManifest(src)
	if err != nil {
		return nil, err
	}
	return maps.Clone(m), nil
}

// Templates is the embedded template trees, for the tests and the
// generator.
func Templates() fs.FS { return templatesFS }
