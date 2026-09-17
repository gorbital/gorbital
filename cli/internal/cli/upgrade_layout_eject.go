package cli

import (
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// builtinKeep is a built-in module a v0.1 app changed, kept as the app's
// own code: the library's module copied in as orb eject copies it, with the
// app's changes carried over.
type builtinKeep struct {
	// Module is the built-in module's name, such as auth.
	Module string `json:"module"`
	// Package and Version are the library package the copy came from.
	Package string `json:"package"`
	Version string `json:"version"`
	// Files are the module's files in the app, by app-relative path.
	Files map[string][]byte `json:"-"`
	// Migrations are the module's migrations to write into db/migrations.
	Migrations map[string][]byte `json:"-"`
	// SHA256 hashes the library package the copy came from, for
	// gorbital.lock (orb doctor compares it).
	SHA256 string `json:"sha256"`
	// Carried lists the app's changes the conversion moved into the copy.
	Carried []string `json:"carried,omitempty"`
	// Failed lists changes it couldn't move, with their diff.
	Failed []string `json:"failed,omitempty"`
	// Hooks are the sign-in options and hooks a change could use instead
	// (Phase 6 of the v0.2 roadmap).
	Hooks []string `json:"hooks,omitempty"`
	// Notes are what orb eject's machinery reported, such as migrations the
	// app already has.
	Notes []string `json:"notes,omitempty"`
}

// authHooks maps a file of the sign-in module to the options and hooks that
// can replace a change to it, so the report can say what a customisation
// could become instead of owned code (ADR-0083, Phase 6).
var authHooks = []struct {
	match string
	hooks []string
}{
	{"usecase/login", []string{"authhttp.BeforeLogin", "authhttp.AfterLogin", "authhttp.RequireMFA"}},
	{"usecase/login_mfa", []string{"authhttp.RequireMFA", "authhttp.BeforeLogin"}},
	{"usecase/register", []string{"authhttp.OnRegister", "authhttp.RegisterFields", "authhttp.WithoutRegistration"}},
	{"usecase/password", []string{"authhttp.PasswordPolicy", "authhttp.MinPasswordLength"}},
	{"usecase/account", []string{"authhttp.OnRegister", "authhttp.RegisterFields"}},
	{"usecase/apikeys", []string{"authhttp.APIKeyMaxTTL"}},
	{"usecase/mfa", []string{"authhttp.RequireMFA"}},
	{"usecase/sign_in", []string{"authhttp.Authenticator.SignIn"}},
	{"usecase/social", []string{"authhttp.BeforeLogin", "authhttp.OnRegister"}},
	{"delivery/routes", []string{"authhttp.RouteMiddleware"}},
	{"delivery/", []string{"authhttp.RouteMiddleware"}},
	{"mail", []string{"authhttp.Brand"}},
}

// keepBuiltinModule copies the library's module into the app the way orb
// eject does, and moves the app's changes to its generated v0.1 code into
// the copy: each change is found in the library's file by its surrounding
// lines and applied there. Changes it can't place are reported with their
// diff, and the module isn't converted.
func keepBuiltinModule(app appInfo, m ejectableModule, lib librarySource, owned []ejectableModule, base, ours map[string][]byte, now time.Time) (builtinKeep, error) {
	keep := builtinKeep{Module: m.name, Package: m.importPath(), Version: lib.Version, Files: map[string][]byte{}, Migrations: map[string][]byte{}}
	rewrite := ejectImportRewriter(app.module, owned)
	res := ejectResult{Files: []string{}, Migrations: []string{}, Modified: []string{}, NotCopied: []ejectSkipped{}, Notes: []string{}}
	src := filepath.Join(lib.Dir, m.pkg)
	copied, hash, err := copyEjectedModule(src, m, rewrite, &res)
	if err != nil {
		return keep, err
	}
	keep.SHA256 = hash
	for _, c := range copied {
		keep.Files[c.Path] = c.Content
	}
	migrations, err := ejectMigrations(app.dir, lib.Dir, src, m, &res)
	if err != nil {
		return keep, err
	}
	for _, c := range migrations {
		keep.Migrations[c.Path] = c.Content
	}
	keep.Notes = res.Notes

	prefix := m.dir() + "/"
	for _, p := range slices.Sorted(maps.Keys(base)) {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		current, ok := ours[p]
		switch {
		case !ok:
			keep.Failed = append(keep.Failed, fmt.Sprintf("%s was deleted; the library's module has it", p))
			continue
		case bytes.Equal(current, base[p]):
			continue
		}
		target, ok := keep.Files[p]
		if !ok {
			keep.Failed = append(keep.Failed, fmt.Sprintf("%s changed, and the library's module has no such file", p))
			continue
		}
		merged, carried, failed := transplant(base[p], current, target)
		if len(failed) > 0 {
			for _, f := range failed {
				keep.Failed = append(keep.Failed, fmt.Sprintf("%s: a change doesn't apply to the library's file:\n%s", p, f))
			}
			continue
		}
		keep.Files[p] = merged
		keep.Carried = append(keep.Carried, fmt.Sprintf("%s: %s", p, changeCount(carried)))
		keep.Hooks = append(keep.Hooks, hooksFor(m, p)...)
	}
	for _, p := range slices.Sorted(maps.Keys(ours)) {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		if _, inBase := base[p]; inBase {
			continue
		}
		if _, clash := keep.Files[p]; clash {
			keep.Failed = append(keep.Failed, fmt.Sprintf("%s is yours and the library's module has a file of that name", p))
			continue
		}
		keep.Files[p] = ours[p]
		keep.Carried = append(keep.Carried, p+": yours, copied as it is")
	}
	slices.Sort(keep.Hooks)
	keep.Hooks = slices.Compact(keep.Hooks)
	_ = now
	return keep, nil
}

func changeCount(n int) string {
	if n == 1 {
		return "1 change carried over"
	}
	return fmt.Sprintf("%d changes carried over", n)
}

// hooksFor returns the options and hooks a change to a file could use
// instead of owning the module's code.
func hooksFor(m ejectableModule, path string) []string {
	if m.name != "auth" {
		return nil
	}
	rest := strings.TrimPrefix(path, m.dir()+"/")
	for _, h := range authHooks {
		if strings.HasPrefix(rest, h.match) {
			return h.hooks
		}
	}
	return nil
}

// hunk is one change between two versions of a file: the lines of the first
// version it replaces, and the lines that replace them.
type hunk struct {
	start, end int
	added      []string
}

// fileHunks returns the changes from a to b, line by line.
func fileHunks(a, b []string) []hunk {
	edits := lineEdits(a, b)
	var hunks []hunk
	line := 0
	for i := 0; i < len(edits); {
		if edits[i].op == ' ' {
			line++
			i++
			continue
		}
		h := hunk{start: line}
		for ; i < len(edits) && edits[i].op != ' '; i++ {
			switch edits[i].op {
			case '-':
				line++
			case '+':
				h.added = append(h.added, edits[i].line)
			}
		}
		h.end = line
		hunks = append(hunks, h)
	}
	return hunks
}

// lineEdit is one line of a comparison: ' ' kept, '-' removed, '+' added.
type lineEdit struct {
	op   byte
	line string
}

// lineEdits compares a and b by their longest common subsequence of lines.
func lineEdits(a, b []string) []lineEdit {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var edits []lineEdit
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			edits = append(edits, lineEdit{' ', a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			edits = append(edits, lineEdit{'-', a[i]})
			i++
		default:
			edits = append(edits, lineEdit{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		edits = append(edits, lineEdit{'-', a[i]})
	}
	for ; j < m; j++ {
		edits = append(edits, lineEdit{'+', b[j]})
	}
	return edits
}

// transplant applies the changes between base and ours to target: each
// change is placed by the lines around it, which must appear exactly once
// in target. It returns the result, how many changes it carried, and the
// diff of every change it couldn't place.
func transplant(base, ours, target []byte) ([]byte, int, []string) {
	baseLines, ourLines, targetLines := splitFileLines(base), splitFileLines(ours), splitFileLines(target)
	hunks := fileHunks(baseLines, ourLines)
	type placed struct {
		at, end int
		lines   []string
	}
	var edits []placed
	var failed []string
	// The lines around a change place it in the library's file. The context
	// shrinks, and may be on one side only, so a change next to a line the
	// library's version changed still lands where it belongs.
	contexts := [][2]int{{3, 3}, {2, 2}, {1, 1}, {3, 0}, {0, 3}, {2, 0}, {0, 2}, {1, 0}, {0, 1}, {0, 0}}
	for _, h := range hunks {
		ok := false
		for _, c := range contexts {
			from, to := max(0, h.start-c[0]), min(len(baseLines), h.end+c[1])
			window := baseLines[from:to]
			if len(window) == 0 {
				continue
			}
			at := onlyMatch(targetLines, window)
			if at < 0 {
				continue
			}
			replacement := slices.Concat(baseLines[from:h.start], h.added, baseLines[h.end:to])
			edits = append(edits, placed{at, at + len(window), replacement})
			ok = true
			break
		}
		if !ok {
			failed = append(failed, hunkDiff(baseLines, h))
		}
	}
	if len(failed) > 0 {
		return nil, 0, failed
	}
	slices.SortStableFunc(edits, func(a, b placed) int { return a.at - b.at })
	for i := 1; i < len(edits); i++ {
		if edits[i].at < edits[i-1].end {
			return nil, 0, []string{"two changes land on the same lines of the library's file"}
		}
	}
	out := slices.Clone(targetLines)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = slices.Concat(out[:e.at], e.lines, out[e.end:])
	}
	return []byte(strings.Join(out, "\n") + "\n"), len(hunks), nil
}

// onlyMatch returns the index where window appears in lines, or -1 when it
// appears nowhere or more than once.
func onlyMatch(lines, window []string) int {
	at := -1
	for i := 0; i+len(window) <= len(lines); i++ {
		if slices.Equal(lines[i:i+len(window)], window) {
			if at >= 0 {
				return -1
			}
			at = i
		}
	}
	return at
}

// hunkDiff renders a change the conversion couldn't place.
func hunkDiff(base []string, h hunk) string {
	var b strings.Builder
	for _, line := range base[h.start:h.end] {
		b.WriteString("    - " + line + "\n")
	}
	for _, line := range h.added {
		b.WriteString("    + " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitFileLines splits a file into lines, without a trailing empty one.
func splitFileLines(content []byte) []string {
	s := strings.TrimSuffix(string(content), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
