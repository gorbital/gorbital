package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The provenance half of orb doctor --security (ADR-0092 §7): what has
// happened upstream to the code orb new copied into the app.
//
// gorbital.lock records, per copied module, the library version it came
// from and a SHA-256 of that package's source. That is enough to fetch the
// version it came from, copy it again in memory, and say three things the
// app cannot say for itself: which of the files it holds changed upstream,
// which of them it has changed itself, and whether what it holds still
// matches what was recorded. It is not enough to say whether any of those
// changes is a security fix, and this file does not pretend otherwise —
// see securityNotChecked.

// A securityCopy is what orb can work out about one copied module.
type securityCopy struct {
	Module    string `json:"module"`
	Package   string `json:"package"`
	Directory string `json:"directory"`
	// Recorded is the library version the copy was made from, and
	// RecordedDate the day it was made, both from gorbital.lock.
	Recorded     string `json:"recorded_version"`
	RecordedDate string `json:"recorded_date"`
	// Current is the version the app requires now.
	Current string `json:"current_version"`
	Status  string `json:"status"`
	// Held is the number of the package's files the app has.
	Held int `json:"held_files"`
	// ChangedUpstream are the held files that differ between the two
	// versions.
	ChangedUpstream []securityChangedFile `json:"changed_upstream"`
	// AddedUpstream and RemovedUpstream are files the current version has
	// and the copy does not, and the other way round.
	AddedUpstream   []string `json:"added_upstream"`
	RemovedUpstream []string `json:"removed_upstream"`
	// EditedLocally are held files whose content is no longer what the copy
	// wrote, so porting a change is a merge rather than a copy.
	EditedLocally []string `json:"edited_locally"`
	// Provenance says whether the recorded hash matches the recorded
	// version's source.
	Provenance string `json:"provenance"`
	// Changelog is what the library's CHANGELOG.md says about the package
	// between the two versions.
	Changelog []securityChangelog `json:"changelog"`
	// Undetermined lists what orb could not work out here, and why. It is
	// never empty of things worth saying: a clean entry still says that no
	// tool in this repository can tell a security fix from a rename.
	Undetermined []string `json:"undetermined"`
	Fix          string   `json:"fix,omitempty"`
}

// A securityChangedFile is one file of the copy that changed upstream.
type securityChangedFile struct {
	// Path is the file in the app; Upstream is the same file in the
	// library package.
	Path     string `json:"path"`
	Upstream string `json:"upstream"`
	// Area is the sign-in flow the file's name puts it in, or "". It is a
	// hint from a file name and nothing more.
	Area string `json:"area,omitempty"`
	// Edited reports that the app changed this file too.
	Edited bool `json:"edited_locally"`
	// Gone reports that the current version no longer has the file.
	Gone bool `json:"removed_upstream,omitempty"`
}

// A securityChangelog is one release's entries naming the package.
type securityChangelog struct {
	Version string   `json:"version"`
	Date    string   `json:"date,omitempty"`
	Entries []string `json:"entries"`
}

// securitySensitiveAreas map a word in a file's name to the flow it
// belongs to. They produce a hint beside a changed file and nothing else:
// no severity, no verdict, and no effect on the exit code. A rename of a
// file called login.go lands here exactly as a fix to it would, which is
// why the report says so wherever the hint appears.
var securitySensitiveAreas = []struct{ word, area string }{
	{"login", "login"},
	{"signin", "login"},
	{"session", "sessions"},
	{"register", "registration"},
	{"registration", "registration"},
	{"signup", "registration"},
	{"verify", "email verification"},
	{"verification", "email verification"},
	{"password", "passwords"},
	{"reset", "password reset"},
	{"mfa", "two-factor"},
	{"totp", "two-factor"},
	{"recovery", "two-factor"},
	{"apikey", "API keys"},
	{"apikeys", "API keys"},
	{"key", "API keys"},
	{"keys", "API keys"},
	{"passkey", "passkeys"},
	{"passkeys", "passkeys"},
	{"webauthn", "passkeys"},
	{"oauth", "social sign-in"},
	{"social", "social sign-in"},
	{"token", "tokens"},
	{"tokens", "tokens"},
	{"invitation", "invitations"},
	{"invitations", "invitations"},
	{"member", "membership"},
	{"members", "membership"},
}

// sensitiveArea returns the flow a copied file's path puts it in, from its
// name alone, or "".
func sensitiveArea(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".go")
	base = strings.TrimSuffix(base, ".sql")
	words := splitWords(strings.ReplaceAll(base, "-", "_"))
	for _, a := range securitySensitiveAreas {
		if slices.Contains(words, a.word) {
			return a.area
		}
	}
	return ""
}

// copies works out, for each module gorbital.lock records as copied, what
// has happened to it upstream.
func (s *securityScan) copies(ctx context.Context, app appInfo) []securityCopy {
	lock, err := readLock(app.dir)
	if err != nil || len(lock.Ejected) == 0 {
		return nil
	}
	var owned []ejectableModule
	for _, e := range lock.Ejected {
		if m, ok := lookupEjectable(e.Module); ok {
			owned = append(owned, m)
		}
	}
	rewrite := ejectImportRewriter(app.module, owned)

	current, currentErr := doctorModule(ctx, app.dir, gorbitalImportPath)
	out := make([]securityCopy, 0, len(lock.Ejected))
	for _, e := range lock.Ejected {
		m, ok := lookupEjectable(e.Module)
		if !ok {
			continue
		}
		c := securityCopy{
			Module: m.name, Package: m.importPath(), Directory: m.dir(),
			Recorded: e.Version, RecordedDate: e.Date, Status: doctorOK,
			ChangedUpstream: []securityChangedFile{}, AddedUpstream: []string{},
			RemovedUpstream: []string{}, EditedLocally: []string{},
			Changelog: []securityChangelog{}, Undetermined: []string{},
		}
		if info, err := os.Stat(filepath.Join(app.dir, filepath.FromSlash(m.dir()))); err != nil || !info.IsDir() {
			c.Status = doctorFail
			c.Undetermined = append(c.Undetermined, m.dir()+" is missing, so nothing about this module could be compared")
			c.Fix = "restore it from git history: gorbital.lock says the app owns it and the app's imports name it"
			out = append(out, c)
			continue
		}
		if currentErr != nil {
			c.Status = doctorWarn
			c.Undetermined = append(c.Undetermined, "the library the app requires couldn't be read ("+firstLine(currentErr.Error())+"), so nothing upstream could be compared")
			out = append(out, c)
			continue
		}
		c.Current = current.Version
		s.compareCopy(ctx, app, m, e, current, rewrite, &c)
		out = append(out, c)
	}
	return out
}

// compareCopy fills in everything that needs the two versions' source.
func (s *securityScan) compareCopy(ctx context.Context, app appInfo, m ejectableModule, e lockEjected, current librarySource, rewrite func(string) (string, string, bool), c *securityCopy) {
	recorded, err := libraryAtVersion(ctx, app.dir, e.Version, current)
	if err != nil {
		c.Status = doctorWarn
		c.Undetermined = append(c.Undetermined,
			fmt.Sprintf("%s %s, the version the copy was made from, couldn't be fetched (%s), so what changed upstream is unknown; the hash in gorbital.lock still says whether the library's package has moved at all", gorbitalImportPath, e.Version, firstLine(err.Error())))
		return
	}
	was, err := copiedPackageFiles(filepath.Join(recorded.Dir, m.pkg))
	if err != nil {
		c.Status = doctorWarn
		c.Undetermined = append(c.Undetermined, fmt.Sprintf("%s %s has no %s to compare with: %s", gorbitalImportPath, e.Version, m.pkg, firstLine(err.Error())))
		return
	}
	now, err := copiedPackageFiles(filepath.Join(current.Dir, m.pkg))
	if err != nil {
		c.Status = doctorWarn
		c.Undetermined = append(c.Undetermined, fmt.Sprintf("%s %s has no %s to compare with: %s", gorbitalImportPath, current.Version, m.pkg, firstLine(err.Error())))
		return
	}

	// Provenance: the source of the recorded version, hashed the way the
	// copy hashed it, against what gorbital.lock recorded.
	switch hash, err := hashLibraryPackage(filepath.Join(recorded.Dir, m.pkg)); {
	case err != nil:
		c.Provenance = "unverified: " + firstLine(err.Error())
	case hash == e.SHA256:
		c.Provenance = "verified: " + m.pkg + " at " + e.Version + " hashes to what gorbital.lock recorded"
	default:
		c.Provenance = "does not match: " + m.pkg + " at " + e.Version + " no longer hashes to what gorbital.lock recorded, so the entry, or the module cache, is not what it was"
		c.Status = doctorWarn
	}

	// The files the app actually holds, and what has happened to each.
	edited := map[string]bool{}
	for _, target := range slices.Sorted(maps.Keys(was)) {
		path := m.dir() + "/" + target
		held, err := os.ReadFile(filepath.Join(app.dir, filepath.FromSlash(path)))
		if err != nil {
			continue // the app doesn't have this file: nothing to say about it
		}
		c.Held++
		wanted, err := copiedContent(target, was[target], rewrite)
		if err == nil && !sameCopiedFile(target, held, wanted) {
			edited[target] = true
			c.EditedLocally = append(c.EditedLocally, path)
		}
		after, still := now[target]
		if still && bytes.Equal(was[target], after) {
			continue
		}
		c.ChangedUpstream = append(c.ChangedUpstream, securityChangedFile{
			Path: path, Upstream: m.pkg + "/" + target, Area: sensitiveArea(target),
			Edited: edited[target], Gone: !still,
		})
		if !still {
			c.RemovedUpstream = append(c.RemovedUpstream, m.pkg+"/"+target)
		}
	}
	for _, target := range slices.Sorted(maps.Keys(now)) {
		if _, had := was[target]; !had {
			c.AddedUpstream = append(c.AddedUpstream, m.pkg+"/"+target)
		}
	}

	c.Changelog = changelogReleases(ctx, s.dir, m.pkg, e.Version)
	c.Undetermined = append(c.Undetermined,
		"which of these changes are security fixes: gorbital publishes no advisory feed, so orb can say that your copy has diverged and where to read, and nothing more")
	if len(c.ChangedUpstream) > 0 {
		c.Status = doctorWarn
		c.Fix = fmt.Sprintf("diff -ru %s %s   # then port what you need into %s, by hand: it is your code",
			filepath.Join(recorded.Dir, m.pkg), filepath.Join(current.Dir, m.pkg), m.dir())
	} else if c.Status == doctorOK && e.Version != current.Version {
		c.Fix = ""
	}
}

// libraryAtVersion returns the source of gorbital.dev/gorbital at version,
// from the module cache or by downloading it. The version the app already
// requires is returned as it is, so an app built against a checkout —
// where the version is not a version at all — still compares against
// itself.
func libraryAtVersion(ctx context.Context, dir, version string, current librarySource) (librarySource, error) {
	if version == current.Version {
		return current, nil
	}
	if !strings.HasPrefix(version, "v") {
		return librarySource{}, fmt.Errorf("gorbital.lock records %q, which isn't a published version", version)
	}
	module := gorbitalImportPath + "@" + version
	out, errOut, err := doctorCommand(ctx, dir, nil, "go", "mod", "download", "-json", module)
	if err != nil && out == "" {
		return librarySource{}, fmt.Errorf("go mod download %s: %w: %s", module, err, firstLine(errOut))
	}
	var mod struct{ Dir, Error string }
	if err := json.Unmarshal([]byte(out), &mod); err != nil {
		return librarySource{}, err
	}
	if mod.Dir == "" {
		return librarySource{}, fmt.Errorf("go mod download %s: %s", module, cmpOr(mod.Error, "no source directory"))
	}
	return librarySource{Version: version, Dir: mod.Dir}, nil
}

// copiedPackageFiles returns the files a library package contributes to an
// app, keyed by their path under the app's module directory: the mapping
// copyEjectedModule applies, without the import rewriting, so the two
// versions can be compared as the library wrote them.
// TestCopiedPackageFilesMatchTheCopy keeps it in step with the copy.
func copiedPackageFiles(src string) (map[string][]byte, error) {
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no %s directory", filepath.Base(src))
	}
	files := map[string][]byte{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		content, err := os.ReadFile(p) //nolint:gosec // the library's source, found by go list
		if err != nil {
			return err
		}
		target := rel
		if rest, ok := strings.CutPrefix(rel, "internal/"); ok {
			if !strings.Contains(rest, "/") {
				return nil // copyEjectedModule refuses this; nothing to compare
			}
			target = rest
		}
		if strings.HasSuffix(rel, ".go") {
			if _, skip := noEjectReason(content); skip {
				return nil
			}
		}
		files[target] = content
		return nil
	})
	return files, err
}

// copiedContent returns a library file as the copy wrote it into the app:
// Go files with their imports pointing at the app's modules, everything
// else byte for byte.
func copiedContent(target string, content []byte, rewrite func(string) (string, string, bool)) ([]byte, error) {
	if !strings.HasSuffix(target, ".go") {
		return content, nil
	}
	out, _, err := rewriteGoImports(target, content, rewrite)
	return out, err
}

// sameCopiedFile reports whether the app's file is still what the copy
// wrote: Go files compared without their import layout, everything else
// byte for byte.
func sameCopiedFile(target string, held, wanted []byte) bool {
	if !strings.HasSuffix(target, ".go") {
		return bytes.Equal(held, wanted)
	}
	return sameGoSource(held, wanted)
}

// sameGoSource reports whether two Go files say the same thing, ignoring
// how their imports are grouped and ordered. Copying a package into an app
// rewrites import paths and then puts each moved import in the group it
// now belongs to, and which group that is has changed between releases of
// orb itself. Counting that as an edit the developer made would put most
// of a module in the "you changed this" list and make the list useless, so
// the import block is compared as a set of paths and the rest byte for
// byte.
func sameGoSource(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	importsA, bodyA, okA := goImportsAndBody(a)
	importsB, bodyB, okB := goImportsAndBody(b)
	return okA && okB && slices.Equal(importsA, importsB) && bytes.Equal(bodyA, bodyB)
}

// goImportsAndBody splits a Go file into its imports, sorted, and
// everything after them.
func goImportsAndBody(src []byte) ([]string, []byte, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, nil, false
	}
	var paths []string
	end := fset.Position(file.Name.End()).Offset
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, nil, false
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name + " "
		}
		paths = append(paths, name+path)
		end = max(end, fset.Position(spec.End()).Offset)
	}
	slices.Sort(paths)
	rest := src[min(end, len(src)):]
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	}
	return paths, bytes.TrimLeft(rest, ")\n\t "), true
}

// changelogReleases returns the library's changelog entries naming pkg in
// the releases after version, newest first, with the date in each heading.
func changelogReleases(ctx context.Context, dir, pkg, version string) []securityChangelog {
	root, err := doctorModule(ctx, dir, "gorbital.dev")
	if err != nil {
		return []securityChangelog{}
	}
	data, err := os.ReadFile(filepath.Join(root.Dir, "CHANGELOG.md"))
	if err != nil {
		return []securityChangelog{}
	}
	return changelogSections(string(data), pkg, version)
}

// changelogSections reads a Keep a Changelog file and returns, for each
// release after version, the entries that name pkg. A release that says
// nothing about the package is left out, and so is its heading: the
// operator is being asked to read, so the list has to be worth reading.
func changelogSections(changelog, pkg, version string) []securityChangelog {
	out := []securityChangelog{}
	var current *securityChangelog
	for line := range strings.Lines(changelog) {
		line = strings.TrimRight(line, "\r\n")
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			current = nil
			v := changelogVersion.FindString(heading)
			if v == "" || v == version || !versionAtLeast(strings.TrimPrefix(v, "v"), strings.TrimPrefix(version, "v")) {
				continue
			}
			out = append(out, securityChangelog{Version: v, Date: changelogDate(heading), Entries: []string{}})
			current = &out[len(out)-1]
			continue
		}
		entry, ok := strings.CutPrefix(strings.TrimLeft(line, " "), "- ")
		if current == nil || !ok || !strings.Contains(entry, pkg) {
			continue
		}
		if r := []rune(entry); len(r) > 160 {
			entry = string(r[:159]) + "…"
		}
		current.Entries = append(current.Entries, entry)
	}
	return slices.DeleteFunc(out, func(c securityChangelog) bool { return len(c.Entries) == 0 })
}

// changelogDate returns the date in a changelog heading, "## v0.2.1
// (2026-10-01)", or "".
func changelogDate(heading string) string {
	_, rest, ok := strings.Cut(heading, "(")
	if !ok {
		return ""
	}
	date, _, ok := strings.Cut(rest, ")")
	if !ok || len(date) != len("2006-01-02") {
		return ""
	}
	return date
}
