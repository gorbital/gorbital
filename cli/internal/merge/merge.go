// Package merge plans how aps upgrade and aps add carry template changes
// into an app the developer has edited: a 3-way merge per file between what
// aps wrote before (base), what it writes now (theirs) and the file on disk
// (ours). Developer edits are never dropped: overlapping changes become
// conflict markers (ADR-0016, ADR-0050).
package merge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// An Action is what happens to one file.
type Action string

// Actions, from nothing to do to needing the developer.
const (
	// Unchanged: nothing to write; the file already holds the result.
	Unchanged Action = "unchanged"
	// Update: the developer never edited the file; it takes the new template.
	Update Action = "update"
	// Create: a new template file.
	Create Action = "create"
	// Delete: a template file removed in the new release, never edited.
	Delete Action = "delete"
	// Merged: both sides changed different lines; merged cleanly.
	Merged Action = "merged"
	// Conflict: both sides changed the same lines; Content holds markers.
	Conflict Action = "conflict"
	// Kept: the new release removed or changed a file the developer deleted
	// or edited in a way that can't be applied; left as it is, with a Note.
	Kept Action = "kept"
)

// A Change is the planned result for one file.
type Change struct {
	Path   string `json:"path"`
	Action Action `json:"action"`
	// Content is what to write for Update, Create, Merged and Conflict.
	Content []byte `json:"-"`
	// Note explains a Kept file or an unproven base.
	Note string `json:"note,omitempty"`
}

// Writes reports whether the change writes Content to the file.
func (c Change) Writes() bool {
	switch c.Action {
	case Update, Create, Merged, Conflict:
		return true
	}
	return false
}

// Input is what Plan merges.
type Input struct {
	// Base is what aps wrote at the old release, by path.
	Base map[string][]byte
	// Theirs is what aps writes at the new release, by path.
	Theirs map[string][]byte
	// Unproven lists base files whose rebuilt content didn't match the hash
	// aps recorded, so they can't be trusted to tell edits from templates.
	Unproven map[string]bool
	// Ours reads the file at path from the app; ok is false when it doesn't
	// exist.
	Ours func(path string) (content []byte, ok bool, err error)
	// Label names the new release in conflict markers, such as
	// "apistock v0.5.0".
	Label string
}

// Plan returns the change for every path in Base or Theirs, sorted by path.
// Migrations under db/migrations are never merged: new ones are created,
// released ones are never changed or deleted.
func Plan(ctx context.Context, in Input) ([]Change, error) {
	paths := slices.Sorted(maps.Keys(in.Theirs))
	for p := range in.Base {
		if _, ok := in.Theirs[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)

	changes := make([]Change, 0, len(paths))
	for _, p := range paths {
		c, err := planFile(ctx, in, p)
		if err != nil {
			return nil, fmt.Errorf("merge %s: %w", p, err)
		}
		changes = append(changes, c)
	}
	return changes, nil
}

func planFile(ctx context.Context, in Input, p string) (Change, error) {
	base, hasBase := in.Base[p]
	theirs, hasTheirs := in.Theirs[p]
	ours, hasOurs, err := in.Ours(p)
	if err != nil {
		return Change{}, err
	}
	proven := hasBase && !in.Unproven[p]
	c := Change{Path: p, Action: Unchanged}
	if hasBase && !proven {
		c.Note = "couldn't prove what aps wrote before, so both versions are compared"
	}

	if isMigration(p) {
		switch {
		case hasBase && hasTheirs && proven && !bytes.Equal(base, theirs):
			return Change{}, errors.New("the new release changes a released migration; released migrations must never change (report this as an apistock bug)")
		case hasTheirs && !hasBase && !hasOurs:
			c.Action, c.Content = Create, theirs
		case hasTheirs && hasOurs && !bytes.Equal(ours, theirs) && !hasBase:
			c.Action, c.Note = Kept, "a migration with this name already exists and differs; rename yours"
		}
		return c, nil // removed migrations stay: the database already ran them
	}

	switch {
	case !hasTheirs: // removed in the new release
		switch {
		case !hasOurs:
		case proven && bytes.Equal(ours, base):
			c.Action = Delete
		default:
			c.Action, c.Note = Kept, "the new release removes this file; kept because it differs from what aps wrote"
		}

	case !hasOurs: // deleted by the developer, or new
		switch {
		case hasBase && proven && bytes.Equal(base, theirs):
		case hasBase:
			c.Action, c.Note = Kept, "you deleted this file and the new release changes it; compare with the release if you need the change"
		default:
			c.Action, c.Content = Create, theirs
		}

	case bytes.Equal(ours, theirs):
	case proven && bytes.Equal(ours, base):
		c.Action, c.Content = Update, theirs
	case proven && bytes.Equal(base, theirs):

	default:
		var mergeBase []byte // no trusted base: a 2-way comparison
		if proven {
			mergeBase = base
		}
		merged, conflicts, err := mergeFile(ctx, ours, mergeBase, theirs, in.Label)
		if err != nil {
			return Change{}, err
		}
		c.Content = merged
		if conflicts > 0 {
			c.Action = Conflict
		} else {
			c.Action = Merged
		}
	}
	return c, nil
}

func isMigration(p string) bool {
	return path.Dir(p) == "db/migrations" && path.Ext(p) == ".sql"
}

// mergeFile runs git merge-file on ours, base and theirs and returns the
// result and the number of conflicts.
func mergeFile(ctx context.Context, ours, base, theirs []byte, label string) ([]byte, int, error) {
	dir, err := os.MkdirTemp("", "aps-merge-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	files := []string{filepath.Join(dir, "ours"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs")}
	for i, content := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(files[i], content, 0o600); err != nil {
			return nil, 0, err
		}
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "merge-file", "-p", "-L", "yours", "-L", "before", "-L", label, files[0], files[1], files[2])
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return stdout.Bytes(), 0, nil
	case errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		return stdout.Bytes(), exit.ExitCode(), nil
	default:
		return nil, 0, fmt.Errorf("git merge-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
}
