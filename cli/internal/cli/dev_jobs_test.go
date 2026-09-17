package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

func TestGenJobKinds(t *testing.T) {
	newFullApp(t)
	cases := []struct {
		name   string
		args   []string
		worker []string // in internal/jobs/<pkg>/<pkg>.go
		def    []string // in internal/app/job_<name>.go
	}{
		{"PingHook", []string{"--kind", "http", "--url", "https://example.com/hook", "--method", "post", "--body", `{"ping":true}`},
			[]string{`Method = "POST"`, `URL    = "https://example.com/hook"`, `Body   = "{\"ping\":true}"`, "client *http.Client"},
			[]string{`//orb:job {"kind":"http","http_method":"POST","http_url":"https://example.com/hook","http_body":"{\"ping\":true}","worker":"sha256:`, "NewWorker(deps.logger, deps.httpClient)"}},
		{"PurgeDrafts", []string{"--kind", "sql", "--sql", "DELETE FROM drafts WHERE updated_at < now() - interval '30 days'"},
			[]string{`const Statement = "DELETE FROM drafts WHERE updated_at < now() - interval '30 days'"`, "pool   *pgxpool.Pool"},
			[]string{`//orb:job {"kind":"sql","sql":"DELETE FROM drafts`, "NewWorker(deps.logger, deps.pool)"}},
		{"WeeklyDigest", []string{"--kind", "email", "--to", "ops@example.com", "--subject", "Weekly digest", "--text", "All is well."},
			[]string{`To      = "ops@example.com"`, `Subject = "Weekly digest"`, "mailer mail.Sender", "IdempotencyKey"},
			[]string{`//orb:job {"kind":"email","email_to":"ops@example.com","email_subject":"Weekly digest","email_text":"All is well.","worker":"sha256:`, "NewWorker(deps.logger, deps.mailer)"}},
		{"NightlyChain", []string{"--kind", "dispatch", "--dispatch", "PurgeDrafts"},
			[]string{`const Target = "purge_drafts"`, "run    func(ctx context.Context, name string) error"},
			[]string{`//orb:job {"kind":"dispatch","dispatch_target":"purge_drafts","worker":"sha256:`, "NewWorker(deps.logger, deps.runJob)"}},
		{"Plain", []string{"--yes"},
			[]string{"func NewWorker(logger *slog.Logger) *Worker"},
			[]string{`//orb:job {"kind":"custom","worker":"sha256:`, "NewWorker(deps.logger)"}},
	}
	for _, c := range cases {
		args := append([]string{"gen", "job", c.name, "--yes"}, c.args...)
		if code, _, errOut := runOrb(t, args...); code != 0 {
			t.Fatalf("orb gen job %s = %d, stderr %q", c.name, code, errOut)
		}
		names, _ := jobNames(c.name)
		worker := readFile(t, filepath.Join("internal", "jobs", names.pkg, names.pkg+".go"))
		for _, want := range c.worker {
			if !strings.Contains(worker, want) {
				t.Errorf("%s worker lacks %q:\n%s", c.name, want, worker)
			}
		}
		def := readFile(t, filepath.Join("internal", "app", "job_"+names.name+".go"))
		for _, want := range c.def {
			if !strings.Contains(def, want) {
				t.Errorf("%s definition lacks %q:\n%s", c.name, want, def)
			}
		}
		marker, ok := recipes.ParseJobMarker([]byte(def))
		if !ok || marker.Worker != recipes.WorkerHash([]byte(worker)) {
			t.Errorf("%s marker = %+v (%v), want the worker's hash", c.name, marker, ok)
		}
	}
}

func TestGenJobKindValidation(t *testing.T) {
	newFullApp(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--kind", "cron"}, "--kind must be one of"},
		{[]string{"--kind", "http"}, "--url must be an http or https URL"},
		{[]string{"--kind", "http", "--url", "ftp://x"}, "--url must be an http or https URL"},
		{[]string{"--kind", "http", "--url", "https://x.test", "--method", "TRACE"}, "--method must be"},
		{[]string{"--kind", "http", "--url", "https://x.test", "--body", "{"}, "--body must be JSON"},
		{[]string{"--kind", "sql"}, "--sql is required"},
		{[]string{"--kind", "email", "--to", "nope"}, "--to must be an email address"},
		{[]string{"--kind", "email", "--to", "a@b.test"}, "--subject is required"},
		{[]string{"--kind", "dispatch", "--dispatch", "1bad"}, "--dispatch"},
		{[]string{"--sql", "SELECT 1"}, "--sql is for another kind of job, not custom"},
		{[]string{"--kind", "sql", "--sql", "SELECT 1", "--to", "a@b.test"}, "--to is for another kind of job, not sql"},
	} {
		args := append([]string{"gen", "job", "Thing", "--yes"}, c.args...)
		code, _, errOut := runOrb(t, args...)
		if code != 2 || !strings.Contains(errOut, c.want) {
			t.Errorf("orb gen job %v = %d %q, want %q", c.args, code, errOut, c.want)
		}
		if _, err := os.Stat(filepath.Join("internal", "jobs", "thing")); err == nil {
			t.Errorf("orb gen job %v wrote files", c.args)
		}
	}
}

func TestJobSources(t *testing.T) {
	dir := newFullApp(t)
	if code, _, errOut := runOrb(t, "gen", "job", "PingHook", "--yes", "--kind", "http", "--url", "https://example.com/hook"); code != 0 {
		t.Fatalf("orb gen job = %d %q", code, errOut)
	}
	// A hand-written job has no marker.
	writeFile(t, filepath.Join(dir, "internal", "app", "job_manual.go"), "package app\n\nimport \"example.com/shop/internal/jobs/manual\"\n\nfunc defineManualJob(defs *jobs.Definitions, deps jobDeps) {}\n")
	writeFile(t, filepath.Join(dir, "internal", "app", "job_manual_test.go"), "package app\n")

	jobs, err := jobSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v, want 2", jobs)
	}
	manual, ping := jobs[0], jobs[1]
	if manual.Name != "manual" || manual.Ident != "Manual" || manual.Package != "manual" || manual.Generated || manual.Ejected || manual.Kind != "custom" ||
		manual.Definition != "internal/app/job_manual.go" || manual.Worker != "internal/jobs/manual/manual.go" || manual.Form != nil {
		t.Errorf("manual = %+v", manual)
	}
	if ping.Name != "ping_hook" || ping.Ident != "PingHook" || ping.Package != "pinghook" || !ping.Generated || ping.Ejected || ping.Kind != "http" ||
		!strings.Contains(string(ping.Form), `"http_url":"https://example.com/hook"`) {
		t.Errorf("ping = %+v", ping)
	}

	// Editing the worker ejects the job: it is a custom job from then on.
	worker := filepath.Join(dir, "internal", "jobs", "pinghook", "pinghook.go")
	src, _ := os.ReadFile(worker)
	writeFile(t, worker, string(src)+"\n// edited by hand\n")
	jobs, err = jobSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ping := jobs[1]; !ping.Generated || !ping.Ejected || ping.Kind != "custom" || ping.Form != nil {
		t.Errorf("ejected = %+v", ping)
	}

	// An app without jobs lists none.
	if jobs, err := jobSources(t.TempDir()); err != nil || len(jobs) != 0 {
		t.Errorf("jobSources(empty) = %v, %v", jobs, err)
	}
}

// TestJobSourcesFromModules lists the jobs an app on gorbital.Main declares
// in its modules, where there is no internal/app (ADR-0083).
func TestJobSourcesFromModules(t *testing.T) {
	dir := t.TempDir()
	write := func(p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/modules/books/module.go", `package books

func Module() gorbital.Module {
	return gorbital.Module{
		Name: "books",
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			jobs.Define(defs, jobs.Definition[digestArgs]{
				Name:        "books.digest",
				Description: "Sends the weekly digest.",
			})
		},
	}
}
`)
	write("internal/modules/books/module_test.go", `package books

// jobs.Define(defs, jobs.Definition[x]{ Name: "books.ignored" })
`)
	found, err := jobSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "books.digest" || found[0].Definition != "internal/modules/books/module.go" || found[0].Package != "books" {
		t.Fatalf("jobSources = %+v", found)
	}
}
