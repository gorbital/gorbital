package recipes

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"strings"
	"text/template"
	"time"
)

//go:embed job/*.tmpl
var jobFS embed.FS

// JobAnchor is the anchor in internal/app/jobs.go that aps gen job adds a
// line after.
const JobAnchor = "//aps:anchor jobs"

// JobData fills the job templates (ADR-0035).
type JobData struct {
	Module      string // app module path, for example example.com/acme-api
	Ident       string // Go identifier, for example CleanupSessions
	Name        string // definition name, for example cleanup_sessions
	Package     string // package name, for example cleanupsessions
	Description string
	Enabled     bool
	Schedule    string // cron, descriptor, "@every 15m", or empty
	Timeout     time.Duration
	MaxAttempts int
	Queue       string
	Priority    int
}

// TimeoutExpr is the timeout as a Go expression, such as 5 * time.Minute.
func (d JobData) TimeoutExpr() string {
	unit := func(n time.Duration, name string) string {
		if n == 1 {
			return "time." + name
		}
		return fmt.Sprintf("%d * time.%s", n, name)
	}
	switch t := d.Timeout; {
	case t%time.Hour == 0:
		return unit(t/time.Hour, "Hour")
	case t%time.Minute == 0:
		return unit(t/time.Minute, "Minute")
	case t%time.Second == 0:
		return unit(t/time.Second, "Second")
	default:
		return unit(t/time.Millisecond, "Millisecond")
	}
}

// JobFile is one rendered job file.
type JobFile struct {
	Path    string
	Content []byte
}

// RenderJob renders the job package, its test and its definition file, in
// that order. Go output is validated with gofmt.
func RenderJob(d JobData) ([]JobFile, error) {
	targets := []struct{ tmpl, path string }{
		{"job/job.go.tmpl", "internal/jobs/" + d.Package + "/" + d.Package + ".go"},
		{"job/job_test.go.tmpl", "internal/jobs/" + d.Package + "/" + d.Package + "_test.go"},
		{"job/definition.go.tmpl", "internal/app/job_" + d.Name + ".go"},
	}
	files := make([]JobFile, 0, len(targets))
	for _, target := range targets {
		src, err := jobFS.ReadFile(target.tmpl)
		if err != nil {
			return nil, err
		}
		tmpl, err := template.New(target.tmpl).Delims("⟦", "⟧").Option("missingkey=error").Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("recipes: parse %s: %w", target.tmpl, err)
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, d); err != nil {
			return nil, fmt.Errorf("recipes: render %s: %w", target.tmpl, err)
		}
		formatted, err := format.Source(out.Bytes())
		if err != nil {
			return nil, fmt.Errorf("recipes: %s is not valid Go after rendering: %w", target.path, err)
		}
		files = append(files, JobFile{Path: target.path, Content: formatted})
	}
	return files, nil
}

// ErrAnchorMissing reports a file without the anchor to insert after.
var ErrAnchorMissing = errors.New("anchor not found")

// InsertAfterAnchor returns src with line inserted after the line holding
// anchor, indented like the anchor. It fails if the anchor is missing or the
// line already exists, and validates the result with gofmt.
func InsertAfterAnchor(src []byte, anchor, line string) ([]byte, error) {
	lines := strings.SplitAfter(string(src), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return nil, fmt.Errorf("%q is already present", strings.TrimSpace(line))
		}
	}
	for i, l := range lines {
		if strings.TrimSpace(l) != anchor {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		out := append(append(append([]string{}, lines[:i+1]...), indent+strings.TrimSpace(line)+"\n"), lines[i+1:]...)
		formatted, err := format.Source([]byte(strings.Join(out, "")))
		if err != nil {
			return nil, fmt.Errorf("result is not valid Go: %w", err)
		}
		return formatted, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrAnchorMissing, anchor)
}
