package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testJobsGo = "package app\n\nfunc defineJobs(defs *jobs.Definitions, deps jobDeps) {\n\t//orb:anchor jobs\n\tdefineHeartbeatJob(defs, deps)\n}\n"

// newFullApp creates a minimal stand-in for a Full preset app and makes it
// the working directory.
func newFullApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "internal", "app", "jobs.go"), testJobsGo)
	t.Chdir(dir)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGenJobWithFlags(t *testing.T) {
	newFullApp(t)
	code, out, errOut := runOrb(t, "gen", "job", "CleanupSessions",
		"--schedule", "30 2 * * *", "--timeout", "5m", "--max-attempts", "8",
		"--description", "Deletes expired sessions.", "--queue", "maintenance", "--priority", "2", "--json")
	if code != 0 {
		t.Fatalf("orb gen job = %d, stderr %q", code, errOut)
	}
	var res genJobResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Definition != "cleanup_sessions" || len(res.Files) != 4 || res.DryRun {
		t.Fatalf("orb gen job --json = %q (%v)", out, err)
	}

	def := readFile(t, filepath.Join("internal", "app", "job_cleanup_sessions.go"))
	for _, want := range []string{
		"func defineCleanupSessionsJob(defs *jobs.Definitions, deps jobDeps)",
		`"example.com/shop/internal/jobs/cleanupsessions"`,
		`Description: "Deletes expired sessions."`,
		"Enabled:     true",
		`Schedule:    "30 2 * * *"`,
		"Timeout:     5 * time.Minute",
		"MaxAttempts: 8",
		`Queue:       "maintenance"`,
		"Priority:    2",
	} {
		if !strings.Contains(def, want) {
			t.Errorf("job_cleanup_sessions.go lacks %q:\n%s", want, def)
		}
	}
	if pkg := readFile(t, filepath.Join("internal", "jobs", "cleanupsessions", "cleanupsessions.go")); !strings.Contains(pkg, `const Name = "cleanup_sessions"`) {
		t.Errorf("job package lacks its name:\n%s", pkg)
	}
	if _, err := os.Stat(filepath.Join("internal", "jobs", "cleanupsessions", "cleanupsessions_test.go")); err != nil {
		t.Errorf("job test not generated: %v", err)
	}
	if jobs := readFile(t, filepath.Join("internal", "app", "jobs.go")); !strings.Contains(jobs, "//orb:anchor jobs\n\tdefineCleanupSessionsJob(defs, deps)\n\tdefineHeartbeatJob(defs, deps)") {
		t.Errorf("jobs.go not registered after the anchor:\n%s", jobs)
	}

	if code, _, errOut := runOrb(t, "gen", "job", "CleanupSessions", "--yes"); code != 1 || !strings.Contains(errOut, "already registered") {
		t.Errorf("generating the same job twice = %d %q, want already registered", code, errOut)
	}
}

func TestGenJobIntervalOnDemandAndDefaults(t *testing.T) {
	newFullApp(t)
	if code, _, errOut := runOrb(t, "gen", "job", "send-report", "--every", "90m", "--disabled", "--yes"); code != 0 {
		t.Fatalf("orb gen job --every = %d %q", code, errOut)
	}
	def := readFile(t, filepath.Join("internal", "app", "job_send_report.go"))
	for _, want := range []string{`Schedule:    "@every 1h30m"`, "Enabled:     false", "Timeout:     time.Minute", "MaxAttempts: 5", `Description: "SendReport job."`, `Queue:       "default"`} {
		if !strings.Contains(def, want) {
			t.Errorf("job_send_report.go lacks %q:\n%s", want, def)
		}
	}

	if code, out, errOut := runOrb(t, "gen", "job", "RebuildIndex", "--on-demand"); code != 0 || !strings.Contains(out, "on demand") {
		t.Fatalf("orb gen job --on-demand = %d %q %q", code, out, errOut)
	}
	if def := readFile(t, filepath.Join("internal", "app", "job_rebuild_index.go")); !strings.Contains(def, `Schedule:    ""`) {
		t.Errorf("on-demand job has a schedule:\n%s", def)
	}

	if code, _, _ := runOrb(t, "gen", "job", "Nightly", "--no-input"); code != 0 {
		t.Fatal("orb gen job with defaults failed")
	}
	if def := readFile(t, filepath.Join("internal", "app", "job_nightly.go")); !strings.Contains(def, `Schedule:    "0 3 * * *"`) {
		t.Errorf("default schedule missing:\n%s", def)
	}
}

func TestGenJobDryRunWritesNothing(t *testing.T) {
	dir := newFullApp(t)
	code, out, errOut := runOrb(t, "gen", "job", "Report", "--every", "15m", "--dry-run")
	if code != 0 || !strings.Contains(out, "Would create (dry run)") || !strings.Contains(out, "internal/app/job_report.go") {
		t.Fatalf("orb gen job --dry-run = %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "app", "job_report.go")); !os.IsNotExist(err) {
		t.Error("dry run wrote the definition file")
	}
	if got := readFile(t, filepath.Join(dir, "internal", "app", "jobs.go")); got != testJobsGo {
		t.Errorf("dry run changed jobs.go:\n%s", got)
	}
}

func TestGenJobValidation(t *testing.T) {
	newFullApp(t)
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"missing generator", []string{"gen"}, 2, "missing generator"},
		{"unknown generator", []string{"gen", "worker"}, 2, "unknown generator"},
		{"missing name", []string{"gen", "job", "--yes"}, 2, "missing job name"},
		{"name starting with digit", []string{"gen", "job", "9Lives"}, 2, "must start with a letter"},
		{"keyword package", []string{"gen", "job", "Default"}, 2, "can't be used as a Go package name"},
		{"template injection", []string{"gen", "job", "x{{.Module}}"}, 2, "must start with a letter"},
		{"two triggers", []string{"gen", "job", "Report", "--schedule", "@daily", "--every", "1h"}, 2, "only one of"},
		{"bad cron", []string{"gen", "job", "Report", "--schedule", "every night"}, 2, "5-field cron"},
		{"interval too short", []string{"gen", "job", "Report", "--every", "30s"}, 2, "at least 1m"},
		{"every too short in schedule", []string{"gen", "job", "Report", "--schedule", "@every 10s"}, 2, "at least 1m"},
		{"timeout too long", []string{"gen", "job", "Report", "--timeout", "48h"}, 2, "between 1s and 24h"},
		{"attempts", []string{"gen", "job", "Report", "--max-attempts", "0"}, 2, "between 1 and 100"},
		{"priority", []string{"gen", "job", "Report", "--priority", "9"}, 2, "between 1 and 4"},
		{"queue", []string{"gen", "job", "Report", "--queue", "bad queue"}, 2, "--queue"},
		{"multi-line description", []string{"gen", "job", "Report", "--description", "a\nb"}, 2, "single line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runOrb(t, tt.args...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("orb %s = %d %q, want %d containing %q", strings.Join(tt.args, " "), code, errOut, tt.wantCode, tt.wantErr)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join("internal", "app")); len(entries) != 1 {
		t.Errorf("failed commands wrote files: %v", entries)
	}
}

func TestGenJobOutsideFullPresetApp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/minimal\n")
	t.Chdir(dir)
	if code, _, errOut := runOrb(t, "gen", "job", "Report", "--yes"); code != 1 || !strings.Contains(errOut, "Full preset") {
		t.Errorf("orb gen job in a Minimal app = %d %q, want Full preset guidance", code, errOut)
	}

	writeFile(t, filepath.Join(dir, "internal", "app", "jobs.go"), "package app\n\nfunc defineJobs() {}\n")
	if code, _, errOut := runOrb(t, "gen", "job", "Report", "--yes"); code != 1 || !strings.Contains(errOut, "//orb:anchor jobs") {
		t.Errorf("orb gen job without the anchor = %d %q, want the line to add", code, errOut)
	}
}

func TestJobNames(t *testing.T) {
	tests := map[string]jobNameSet{
		"Heartbeat":        {ident: "Heartbeat", name: "heartbeat", pkg: "heartbeat"},
		"CleanupSessions":  {ident: "CleanupSessions", name: "cleanup_sessions", pkg: "cleanupsessions"},
		"cleanup-sessions": {ident: "CleanupSessions", name: "cleanup_sessions", pkg: "cleanupsessions"},
		"cleanup_sessions": {ident: "CleanupSessions", name: "cleanup_sessions", pkg: "cleanupsessions"},
		"HTTPSync":         {ident: "HttpSync", name: "http_sync", pkg: "httpsync"},
		"SendReport2":      {ident: "SendReport2", name: "send_report2", pkg: "sendreport2"},
	}
	for input, want := range tests {
		got, err := jobNames(input)
		if err != nil || got != want {
			t.Errorf("jobNames(%q) = %+v, %v; want %+v", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "9lives", "has space", "func", strings.Repeat("a", 61)} {
		if _, err := jobNames(bad); err == nil {
			t.Errorf("jobNames(%q) error = nil", bad)
		}
	}
}

func TestValidateSchedule(t *testing.T) {
	for _, ok := range []string{"0 3 * * *", "*/15 * * * *", "0 9 * * MON-FRI", "@daily", "@hourly", "@every 15m", "@every 2h"} {
		if err := validateSchedule(ok); err != nil {
			t.Errorf("validateSchedule(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "0 3 * *", "every day", "@every 59s", "@fortnightly", "0 3 * * * *"} {
		if err := validateSchedule(bad); err == nil {
			t.Errorf("validateSchedule(%q) = nil", bad)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second: "10s",
		time.Minute:      "1m",
		30 * time.Minute: "30m",
		90 * time.Minute: "1h30m",
		time.Hour:        "1h",
		10 * time.Hour:   "10h",
		90 * time.Second: "1m30s",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
