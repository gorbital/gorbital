// Package merge is a Phase 0 spike. It answers one question: can a 3-way
// merge carry template changes into a generated file that the developer has
// edited, without losing their edits? See README.md for results.
//
// Throwaway code: do not import from anywhere else.
package merge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Outcome describes what an upgrade did to one scaffolded file.
type Outcome string

const (
	KeepOurs   Outcome = "keep-ours"   // the template did not change
	TakeTheirs Outcome = "take-theirs" // the developer never edited the file
	CleanMerge Outcome = "clean-merge" // both changed, merged without conflicts
	Conflict   Outcome = "conflict"    // both changed the same region
)

// Result is the merged content of one file.
type Result struct {
	Content   []byte
	Outcome   Outcome
	Conflicts int
}

// Upgrade merges a template change (base -> theirs) into the developer's
// file (ours). base is what the generator originally wrote, theirs is what
// the new template version would write today.
func Upgrade(base, ours, theirs []byte) (Result, error) {
	switch {
	case bytes.Equal(ours, base):
		return Result{Content: theirs, Outcome: TakeTheirs}, nil
	case bytes.Equal(base, theirs):
		return Result{Content: ours, Outcome: KeepOurs}, nil
	}

	dir, err := os.MkdirTemp("", "aps-merge-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)

	files := map[string][]byte{"ours": ours, "base": base, "theirs": theirs}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			return Result{}, err
		}
	}

	cmd := exec.Command("git", "merge-file", "-p",
		"-L", "yours", "-L", "generated", "-L", "apistock upgrade",
		filepath.Join(dir, "ours"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()

	// git merge-file exits with the number of conflicts, or a negative
	// value (255 on Unix) on error.
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return Result{Content: stdout.Bytes(), Outcome: CleanMerge}, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() > 0 && exitErr.ExitCode() < 128:
		return Result{Content: stdout.Bytes(), Outcome: Conflict, Conflicts: exitErr.ExitCode()}, nil
	default:
		return Result{}, fmt.Errorf("git merge-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
}

// InsertAfterAnchor mimics the generator's InsertAtAnchor operation at text
// level (the AST version is a separate spike). It inserts line directly after
// the "//aps:anchor <name>" comment, using the anchor's indentation.
func InsertAfterAnchor(src []byte, anchor, line string) ([]byte, error) {
	marker := "//aps:anchor " + anchor
	lines := strings.Split(string(src), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != marker {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, "\t "))]
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:i+1]...)
		out = append(out, indent+line)
		out = append(out, lines[i+1:]...)
		return []byte(strings.Join(out, "\n")), nil
	}
	return nil, fmt.Errorf("anchor %q not found", anchor)
}
