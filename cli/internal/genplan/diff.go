package genplan

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines surround each change in Diff.
const diffContext = 3

// Diff returns the plan's changes as a unified diff, the way orb gen
// --diff prints them: a created file is all additions, a modified file shows
// its changed lines with three lines of context.
func Diff(p Plan) string {
	var b strings.Builder
	for _, c := range p.Changes {
		after := splitLines(string(c.Content))
		if c.Kind == Create {
			fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", c.Path, len(after))
			for _, line := range after {
				b.WriteString("+" + line + "\n")
			}
			continue
		}
		before := splitLines(string(c.Before))
		hunks := diffHunks(before, after)
		if len(hunks) == 0 {
			continue
		}
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", c.Path, c.Path)
		for _, h := range hunks {
			b.WriteString(h)
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// edit is one line of a line-by-line comparison: ' ' kept, '-' removed,
// '+' added.
type edit struct {
	op   byte
	line string
}

// lineEdits compares a and b by their longest common subsequence of lines.
// Generated files are small, so the quadratic table is fine.
func lineEdits(a, b []string) []edit {
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
	var edits []edit
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			edits = append(edits, edit{' ', a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			edits = append(edits, edit{'-', a[i]})
			i++
		default:
			edits = append(edits, edit{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		edits = append(edits, edit{'-', a[i]})
	}
	for ; j < m; j++ {
		edits = append(edits, edit{'+', b[j]})
	}
	return edits
}

// diffHunks groups the changed lines of a and b into unified diff hunks.
func diffHunks(a, b []string) []string {
	edits := lineEdits(a, b)
	var hunks []string
	for start := 0; start < len(edits); {
		if edits[start].op == ' ' {
			start++
			continue
		}
		// The hunk runs from diffContext lines before the change to
		// diffContext lines after the last change closer than that.
		from := max(0, start-diffContext)
		end := start
		for k := start; k < len(edits); k++ {
			if edits[k].op != ' ' {
				end = k
			} else if k-end > 2*diffContext {
				break
			}
		}
		to := min(len(edits), end+diffContext+1)
		oldStart, newStart := 1, 1
		for _, e := range edits[:from] {
			if e.op != '+' {
				oldStart++
			}
			if e.op != '-' {
				newStart++
			}
		}
		var body strings.Builder
		oldLen, newLen := 0, 0
		for _, e := range edits[from:to] {
			if e.op != '+' {
				oldLen++
			}
			if e.op != '-' {
				newLen++
			}
			body.WriteString(string(e.op) + e.line + "\n")
		}
		hunks = append(hunks, fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldLen, newStart, newLen)+body.String())
		start = to
	}
	return hunks
}
