package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// bashBlock returns the lines of the fenced bash block of markdown that
// holds want, with each line's trailing # comment removed.
func bashBlock(t *testing.T, markdown, want string) []string {
	t.Helper()
	for _, block := range strings.Split(markdown, "```bash\n")[1:] {
		block, _, _ = strings.Cut(block, "```")
		if !strings.Contains(block, want) {
			continue
		}
		var lines []string
		for _, line := range strings.Split(block, "\n") {
			command, _, _ := strings.Cut(line, "#")
			if command = strings.TrimSpace(command); command != "" {
				lines = append(lines, command)
			}
		}
		return lines
	}
	t.Fatalf("no bash block holding %q in:\n%s", want, markdown)
	return nil
}

// TestNewPrintsTheStepsTheREADMEDoes: the "without orb" steps orb new prints
// are the app's own steps, so a Full app started without the CLI reads .env
// into the environment and has an encryption key before seed runs (CLI-12d).
func TestNewPrintsTheStepsTheREADMEDoes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	code, out, errOut := runOrb(t, "new", "shop-api", "--skip-tidy", "--no-git", "--preset", "full")
	if code != 0 {
		t.Fatalf("orb new --preset full = %d: %s", code, errOut)
	}
	t.Chdir(filepath.Join(dir, "shop-api"))

	readme := readFile(t, "README.md")
	steps := bashBlock(t, readme, "cp .env.example .env")
	if len(steps) < 6 {
		t.Fatalf("README.md lists only %d steps without orb: %v", len(steps), steps)
	}
	for _, step := range steps {
		if !strings.Contains(out, step) {
			t.Errorf("orb new didn't print the README's step %q; it printed:\n%s", step, out)
		}
	}
	if !strings.Contains(out, encryptionKeysVar) {
		t.Errorf("orb new didn't mention %s; it printed:\n%s", encryptionKeysVar, out)
	}
}
