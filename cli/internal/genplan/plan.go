// Package genplan describes what a generator would write before it writes
// anything: a plan of file changes that the CLI prints as a dry run, that
// the Dev Portal shows as a diff to approve, and that [Apply] writes
// (ADR-0021, ADR-0066).
//
// A plan is made from the app as it is now. Applying it checks that every
// file it changes is still as the plan saw it, so a plan made against a
// stale working tree is refused instead of overwriting edits made since.
package genplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Kind is what a change does to its file.
type Kind string

// Kinds of change.
const (
	// Create writes a file that must not exist yet.
	Create Kind = "create"
	// Modify replaces a file's content; Before holds what it replaces.
	Modify Kind = "modify"
)

// Change is one file a plan writes. Paths are slash-separated and
// relative to the app directory.
type Change struct {
	Path string `json:"path"`
	Kind Kind   `json:"kind"`
	// Content is the file after the change.
	Content []byte `json:"-"`
	// Before is the file before a Modify; nil for a Create.
	Before []byte `json:"-"`
}

// MarshalJSON encodes the content as strings, so a UI can show the diff.
func (c Change) MarshalJSON() ([]byte, error) {
	type wire struct {
		Path    string `json:"path"`
		Kind    Kind   `json:"kind"`
		Content string `json:"content"`
		Before  string `json:"before,omitempty"`
	}
	return json.Marshal(wire{Path: c.Path, Kind: c.Kind, Content: string(c.Content), Before: string(c.Before)})
}

// UnmarshalJSON is the inverse of MarshalJSON.
func (c *Change) UnmarshalJSON(data []byte) error {
	var w struct {
		Path    string `json:"path"`
		Kind    Kind   `json:"kind"`
		Content string `json:"content"`
		Before  string `json:"before"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*c = Change{Path: w.Path, Kind: w.Kind, Content: []byte(w.Content)}
	if w.Kind == Modify {
		c.Before = []byte(w.Before)
	}
	return nil
}

// Plan is everything a generator would write, with what to tell the
// developer about it.
type Plan struct {
	// Generator is the generator that made the plan: job, resource or
	// migration.
	Generator string `json:"generator"`
	// Name is what was generated, as the developer named it.
	Name string `json:"name"`
	// Summary is the CLI's confirmation text: what the plan creates.
	Summary string `json:"summary"`
	// Changes are the files, in the order they are written.
	Changes []Change `json:"changes"`
	// Next are the steps to take after applying, one per line.
	Next []string `json:"next"`
	// Result is the generator's --json result, without the schema version.
	Result any `json:"result,omitempty"`
}

// Paths lists the files the plan writes, in order.
func (p Plan) Paths() []string {
	paths := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		paths = append(paths, c.Path)
	}
	return paths
}

// Errors of Apply.
var (
	// ErrExists reports a Create whose file already exists.
	ErrExists = errors.New("file already exists")
	// ErrStale reports a Modify whose file changed since the plan was made.
	ErrStale = errors.New("file changed since the plan was made")
)

// Check reports whether the plan can be applied to dir as it is now:
// files to create don't exist, and files to modify are as the plan saw
// them. Apply runs it first; the portal runs it to warn early.
func Check(dir string, p Plan) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, c := range p.Changes {
		path := filepath.FromSlash(c.Path)
		switch c.Kind {
		case Create:
			if _, err := root.Stat(path); err == nil {
				return fmt.Errorf("%s: %w", c.Path, ErrExists)
			}
		case Modify:
			current, err := root.ReadFile(path)
			if err != nil {
				return fmt.Errorf("%s: %w", c.Path, err)
			}
			if !slices.Equal(current, c.Before) {
				return fmt.Errorf("%s: %w", c.Path, ErrStale)
			}
		default:
			return fmt.Errorf("%s: unknown change kind %q", c.Path, c.Kind)
		}
	}
	return nil
}

// Apply writes the plan's changes into dir, after Check. Files are written
// through os.Root, so no path escapes the app (ADR-0029, threat 3).
func Apply(dir string, p Plan) error {
	if err := Check(dir, p); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, c := range p.Changes {
		path := filepath.FromSlash(c.Path)
		if parent := filepath.Dir(path); parent != "." {
			if err := root.MkdirAll(parent, 0o755); err != nil {
				return fmt.Errorf("%s: %w", c.Path, err)
			}
		}
		if err := root.WriteFile(path, c.Content, 0o644); err != nil {
			return fmt.Errorf("%s: %w", c.Path, err)
		}
	}
	return nil
}

// Describe returns the plan's changes as the CLI lists them: one path per
// line, prefixed with what happens to it.
func Describe(p Plan) string {
	var b strings.Builder
	for _, c := range p.Changes {
		verb := "create"
		if c.Kind == Modify {
			verb = "modify"
		}
		fmt.Fprintf(&b, "  %-6s %s\n", verb, c.Path)
	}
	return b.String()
}
