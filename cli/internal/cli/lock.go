package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"gorbital.dev/cli/internal/recipes"
)

// LockAPIVersion versions the gorbital.lock format (ADR-0015, ADR-0050).
const LockAPIVersion = "gorbital.dev/v2"

// lockAPIVersionV1 is the format early development builds of orb wrote: one createFile
// operation per file, without the release or inputs that rendered them.
const lockAPIVersionV1 = "gorbital.dev/v1"

const lockPath = "gorbital.lock"

// lockFile records what orb rendered into an app, so orb upgrade can rebuild
// those files at the recorded release and merge template changes into the
// developer's edits (ADR-0016, ADR-0050).
type lockFile struct {
	APIVersion string       `json:"apiVersion"`
	Orb        lockOrb      `json:"orb"`
	Inputs     lockInputs   `json:"inputs"`
	Files      []lockedFile `json:"files"`
	// Ejected are the built-in modules orb eject copied into the app, which
	// the app owns from then on (ADR-0083). A lock orb eject creates in an
	// app orb new didn't write has only these.
	Ejected []lockEjected `json:"ejected,omitempty"`
}

// lockEjected records a built-in module orb eject copied into the app.
type lockEjected struct {
	// Module is the name orb eject takes, such as auth; the code is in
	// internal/modules/<Module>.
	Module string `json:"module"`
	// Package is the library package it was copied from, such as
	// gorbital.dev/gorbital/authhttp.
	Package string `json:"package"`
	// Version is the gorbital.dev/gorbital version the app required.
	Version string `json:"version"`
	// Date is the day of the ejection, as YYYY-MM-DD.
	Date string `json:"date"`
	// SHA256 hashes the package's source as orb eject read it, so orb doctor
	// notices when the library's module has changed since.
	SHA256 string `json:"sha256"`
}

// rendered reports whether orb new wrote the lock's app, as opposed to a
// lock orb eject created that records only ejected modules.
func (l lockFile) rendered() bool {
	return l.Inputs != (lockInputs{}) || len(l.Files) > 0
}

// ejected returns the ejection of module, if the lock records one.
func (l lockFile) ejected(module string) (lockEjected, bool) {
	for _, e := range l.Ejected {
		if e.Module == module {
			return e, true
		}
	}
	return lockEjected{}, false
}

// lockOrb is the orb release that rendered the tracked files.
type lockOrb struct {
	Version string `json:"version,omitempty"`
	// Revision is the commit a development build was built from, recorded
	// only when its tree was clean, so the templates are exactly that commit.
	Revision string `json:"revision,omitempty"`
}

// lockInputs are the values the templates read. The --local checkout path
// isn't one: it only reaches go.mod, which is never hashed or merged.
type lockInputs struct {
	Name    string `json:"name"`
	Module  string `json:"module"`
	Preset  string `json:"preset"`
	Tenancy string `json:"tenancy"`
	Mail    string `json:"mail,omitempty"`
	// RLS records orb add rls: gorbital.yaml says rls: true (ADR-0061).
	RLS bool `json:"rls,omitempty"`
	// Layout is the layout whose templates wrote the app: empty for the
	// v0.1 layout (every lock before v0.2, and Minimal apps), or
	// recipes.LayoutV02 for apps on gorbital.Main (ADR-0083).
	Layout string `json:"layout,omitempty"`
}

// layout returns the app's layout, recipes.LayoutV01 or recipes.LayoutV02.
func (in lockInputs) layout() string {
	if in.Layout == "" {
		return recipes.LayoutV01
	}
	return in.Layout
}

// layoutValue is how a lock records layout: v0.1 as no layout, so locks
// of v0.1 apps stay readable by orb v0.1.
func layoutValue(layout string) string {
	if layout == recipes.LayoutV01 {
		return ""
	}
	return layout
}

// validate checks inputs read from source (gorbital.lock or gorbital.yaml)
// with the rules orb new applies before rendering. Anyone who can commit to
// the app can edit those files, and orb upgrade and orb add orgs render the
// name and module into every template, so a module path carrying Go syntax
// would become code in a commit reviewers trust (ADR-0029, threat 2).
func (in lockInputs) validate(source string) error {
	switch {
	case validateName(in.Name) != nil:
		return fmt.Errorf("%s has an invalid app name %q: use lowercase letters, digits and single hyphens, starting with a letter (max %d characters)", source, in.Name, maxNameLength)
	case validateModule(in.Module) != nil:
		return fmt.Errorf("%s has an invalid module path %q", source, in.Module)
	}
	if _, ok := recipes.LookupPreset(in.Preset, in.Tenancy); !ok {
		return fmt.Errorf("%s has an unknown preset %q with tenancy %q", source, in.Preset, in.Tenancy)
	}
	if in.RLS && (in.Preset != "full" || in.Tenancy != recipes.TenancyMulti) {
		return fmt.Errorf("%s records row-level security for an app without organisations", source)
	}
	switch in.Layout {
	case "":
	case recipes.LayoutV02:
		if p, _ := recipes.LookupPreset(in.Preset, in.Tenancy); p.Layout() != recipes.LayoutV02 {
			return fmt.Errorf("%s records the %s layout for the %s preset, which has none", source, in.Layout, in.Preset)
		}
	default:
		return fmt.Errorf("%s has an unknown layout %q", source, in.Layout)
	}
	switch in.Mail {
	case "", recipes.MailResend, recipes.MailSMTP:
		return nil
	}
	return fmt.Errorf("%s has an unknown mail provider %q", source, in.Mail)
}

// lockedFile is a tracked file and the SHA-256 of its content as orb wrote it.
type lockedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// untrackedPaths are rendered but never hashed: go mod tidy and go get
// rewrite them, and upgrades update them with go get instead of merging.
var untrackedPaths = []string{"go.mod", "go.sum"}

// newLock records files rendered from preset with d by this orb.
func newLock(preset recipes.Preset, d recipes.Data, files []recipes.File) lockFile {
	l := lockFile{
		APIVersion: LockAPIVersion,
		Orb:        lockOrb{Version: Version, Revision: buildRevision()},
		Inputs:     lockInputs{Name: d.Name, Module: d.Module, Preset: preset.Name, Tenancy: preset.Tenancy, Layout: layoutValue(preset.Layout())},
	}
	if preset.Name == "full" {
		l.Inputs.Mail = recipes.MailResend // the golden apps send with Resend
	}
	for _, f := range files {
		if !slices.Contains(untrackedPaths, f.Path) {
			l.Files = append(l.Files, lockedFile{Path: f.Path, SHA256: f.SHA256})
		}
	}
	slices.SortFunc(l.Files, compareLocked)
	return l
}

func compareLocked(a, b lockedFile) int { return strings.Compare(a.Path, b.Path) }

func (l lockFile) find(path string) (int, bool) {
	return slices.BinarySearchFunc(l.Files, path, func(f lockedFile, p string) int { return strings.Compare(f.Path, p) })
}

// tracks reports whether path is a tracked file.
func (l lockFile) tracks(path string) bool {
	_, ok := l.find(path)
	return ok
}

// record sets the hash of a tracked file to content's. Untracked paths, such
// as .env or files orb gen wrote, are left out.
func (l *lockFile) record(path string, content []byte) {
	if i, ok := l.find(path); ok {
		l.Files[i].SHA256 = sha256Hex(content)
	}
}

func (l lockFile) encode() ([]byte, error) {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", lockPath, err)
	}
	return append(b, '\n'), nil
}

func writeLock(root *os.Root, l lockFile) error {
	b, err := l.encode()
	if err != nil {
		return err
	}
	if err := root.WriteFile(lockPath, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", lockPath, err)
	}
	return nil
}

// errNoLock reports an app without gorbital.lock.
var errNoLock = errors.New("no " + lockPath)

// readLock reads dir's gorbital.lock. A v1 lock comes back with its files
// and APIVersion v1, but no release or inputs: v1 didn't record them.
func readLock(dir string) (lockFile, error) {
	data, err := os.ReadFile(filepath.Join(dir, lockPath))
	if errors.Is(err, fs.ErrNotExist) {
		return lockFile{}, errNoLock
	} else if err != nil {
		return lockFile{}, err
	}
	var head struct {
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return lockFile{}, fmt.Errorf("read %s: %w", lockPath, err)
	}

	var l lockFile
	switch head.APIVersion {
	case LockAPIVersion:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&l); err != nil {
			return lockFile{}, fmt.Errorf("read %s: %w", lockPath, err)
		}
		if l.rendered() {
			if err := l.Inputs.validate(lockPath); err != nil {
				return lockFile{}, err
			}
		}
		if err := validateEjected(l.Ejected); err != nil {
			return lockFile{}, err
		}
	case lockAPIVersionV1:
		var v1 struct {
			Recipes []struct {
				Operations []struct {
					Op     string `json:"op"`
					Path   string `json:"path"`
					SHA256 string `json:"sha256"`
				} `json:"operations"`
			} `json:"recipes"`
		}
		if err := json.Unmarshal(data, &v1); err != nil {
			return lockFile{}, fmt.Errorf("read %s: %w", lockPath, err)
		}
		l.APIVersion = lockAPIVersionV1
		for _, r := range v1.Recipes {
			for _, op := range r.Operations {
				if op.Op == "createFile" && !slices.Contains(untrackedPaths, op.Path) {
					l.Files = append(l.Files, lockedFile{Path: op.Path, SHA256: op.SHA256})
				}
			}
		}
	default:
		return lockFile{}, fmt.Errorf("%s has apiVersion %q; this orb reads %s and %s (a newer orb may have written it)", lockPath, head.APIVersion, LockAPIVersion, lockAPIVersionV1)
	}

	slices.SortFunc(l.Files, compareLocked)
	for i, f := range l.Files {
		if !fs.ValidPath(f.Path) || f.Path == "." || (i > 0 && l.Files[i-1].Path == f.Path) {
			return lockFile{}, fmt.Errorf("read %s: invalid or repeated path %q", lockPath, f.Path)
		}
	}
	return l, nil
}

// buildRevision returns the commit this binary was built from, or "" for
// builds without version control information and builds of a modified tree.
func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return revisionOf(info)
}

func revisionOf(info *debug.BuildInfo) string {
	var revision string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				return ""
			}
		}
	}
	return revision
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// validateEjected checks the ejected modules a lock records: orb doctor and
// orb gen modules read directories from their names, so a name must be one
// orb eject knows, recorded once.
func validateEjected(ejected []lockEjected) error {
	seen := map[string]bool{}
	for _, e := range ejected {
		m, ok := lookupEjectable(e.Module)
		switch {
		case !ok:
			return fmt.Errorf("%s records an unknown ejected module %q", lockPath, e.Module)
		case seen[e.Module]:
			return fmt.Errorf("%s records the ejected module %q twice", lockPath, e.Module)
		case e.Package != m.importPath():
			return fmt.Errorf("%s records the ejected module %q from %q, want %s", lockPath, e.Module, e.Package, m.importPath())
		}
		seen[e.Module] = true
	}
	return nil
}
