package recipes

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"strings"
	"text/template"
	"time"
)

//go:embed job/*.tmpl
var jobFS embed.FS

// JobAnchor is the anchor in internal/app/jobs.go that orb gen job adds a
// line after.
const JobAnchor = "//orb:anchor jobs"

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

	// Kind is what the job does (ADR-0071): custom (a Work method to
	// write), http (a request), sql (a statement), email (a message) or
	// dispatch (starts another job). Each kind's fields follow.
	Kind           string
	HTTPMethod     string
	HTTPURL        string
	HTTPBody       string
	SQL            string
	EmailTo        string
	EmailSubject   string
	EmailText      string
	DispatchTarget string

	workerHash string // set while rendering
}

// Kinds of generated jobs.
const (
	KindCustom   = "custom"
	KindHTTP     = "http"
	KindSQL      = "sql"
	KindEmail    = "email"
	KindDispatch = "dispatch"
)

// JobKinds lists the kinds.
var JobKinds = []string{KindCustom, KindHTTP, KindSQL, KindEmail, KindDispatch}

// JobMarker is what the Dev Portal reads back from a generated job's
// definition file (the //orb:job line) to show it as a form again.
type JobMarker struct {
	Kind           string `json:"kind"`
	HTTPMethod     string `json:"http_method,omitempty"`
	HTTPURL        string `json:"http_url,omitempty"`
	HTTPBody       string `json:"http_body,omitempty"`
	SQL            string `json:"sql,omitempty"`
	EmailTo        string `json:"email_to,omitempty"`
	EmailSubject   string `json:"email_subject,omitempty"`
	EmailText      string `json:"email_text,omitempty"`
	DispatchTarget string `json:"dispatch_target,omitempty"`
	// Worker is the SHA-256 of the generated worker file: while the file
	// still hashes to it, the portal shows the form; once edited by hand,
	// the job is ejected and edited in code.
	Worker string `json:"worker"`
}

// Marker renders the //orb:job line's JSON.
func (d JobData) Marker() string {
	m := JobMarker{Kind: d.Kind, HTTPMethod: d.HTTPMethod, HTTPURL: d.HTTPURL, HTTPBody: d.HTTPBody, SQL: d.SQL,
		EmailTo: d.EmailTo, EmailSubject: d.EmailSubject, EmailText: d.EmailText, DispatchTarget: d.DispatchTarget, Worker: d.workerHash}
	out, _ := json.Marshal(m)
	return string(out)
}

// WorkerArgs are the arguments the definition passes to NewWorker.
func (d JobData) WorkerArgs() string {
	switch d.Kind {
	case KindHTTP:
		return "deps.logger, deps.httpClient"
	case KindSQL:
		return "deps.logger, deps.pool"
	case KindEmail:
		return "deps.logger, deps.mailer"
	case KindDispatch:
		return "deps.logger, deps.runJob"
	default:
		return "deps.logger"
	}
}

// ParseJobMarker returns the marker in a definition file, or false when
// the file has none (a hand-written job).
func ParseJobMarker(src []byte) (JobMarker, bool) {
	for line := range strings.Lines(string(src)) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "//orb:job ")
		if !ok {
			continue
		}
		var m JobMarker
		if err := json.Unmarshal([]byte(rest), &m); err != nil {
			return JobMarker{}, false
		}
		return m, true
	}
	return JobMarker{}, false
}

// WorkerHash is the hash the marker records for a worker file.
func WorkerHash(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
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
// that order. Go output is validated with gofmt. The definition's marker
// records the worker file's hash.
func RenderJob(d JobData) ([]JobFile, error) {
	if d.Kind == "" {
		d.Kind = KindCustom
	}
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
		if target.tmpl == "job/job.go.tmpl" {
			d.workerHash = WorkerHash(formatted)
		}
		files = append(files, JobFile{Path: target.path, Content: formatted})
	}
	return files, nil
}

// ErrAnchorMissing reports a file without the anchor to insert after.
var ErrAnchorMissing = errors.New("anchor not found")

// ErrLinePresent reports a line that is already in the file.
var ErrLinePresent = errors.New("line already present")

// InsertAfterAnchor returns src with line inserted after the line holding
// anchor, indented like the anchor. It fails if the anchor is missing or the
// line already exists, and validates the result with gofmt.
func InsertAfterAnchor(src []byte, anchor, line string) ([]byte, error) {
	lines := strings.SplitAfter(string(src), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return nil, fmt.Errorf("%q is already present: %w", strings.TrimSpace(line), ErrLinePresent)
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
