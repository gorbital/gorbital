package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeCommands records the commands a devRunner runs instead of running them.
type fakeCommands struct {
	calls   []string
	outputs map[string]string // command line → standard output
	fail    map[string]bool   // command lines that fail
	noTool  bool              // lookPath finds nothing
}

func (f *fakeCommands) install(d *devRunner) {
	record := func(name string, args []string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		f.calls = append(f.calls, line)
		if f.fail[line] {
			return line, errors.New("exit status 1")
		}
		return line, nil
	}
	d.run = func(_ context.Context, _ []string, name string, args ...string) error {
		_, err := record(name, args)
		return err
	}
	d.output = func(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
		line, err := record(name, args)
		return []byte(f.outputs[line]), err
	}
	d.lookPath = func(file string) (string, error) {
		if f.noTool {
			return "", errors.New("executable file not found in $PATH")
		}
		return "/usr/local/bin/" + file, nil
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

// devPorts are free host ports for an app's services, as .env lines.
type devPorts struct {
	app, postgres, smtp, web, grafana, otlp string
}

func newDevPorts(t *testing.T) devPorts {
	return devPorts{freePort(t), freePort(t), freePort(t), freePort(t), freePort(t), freePort(t)}
}

func (p devPorts) env() string {
	return fmt.Sprintf("APP_ADDR=127.0.0.1:%s\nPOSTGRES_PORT=%s\nMAILPIT_SMTP_PORT=%s\nMAILPIT_WEB_PORT=%s\nGRAFANA_PORT=%s\nOTLP_HTTP_PORT=%s\n",
		p.app, p.postgres, p.smtp, p.web, p.grafana, p.otlp)
}

// newDevApp writes a stand-in app with gorbital.yaml, .env.example,
// compose.yaml and cmd/seed, and makes it the working directory.
func newDevApp(t *testing.T, manifest, envExample string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "gorbital.yaml"), manifest)
	writeFile(t, filepath.Join(dir, ".env.example"), envExample)
	writeFile(t, filepath.Join(dir, "compose.yaml"), "services: {}\n")
	writeFile(t, filepath.Join(dir, "cmd", "seed", "main.go"), "package main\n")
	t.Chdir(dir)
	return dir
}

const fullManifest = "preset: full\nfeatures: [postgres, settings, jobs]\n"

func TestDevPrepareFullApp(t *testing.T) {
	ports := newDevPorts(t)
	newDevApp(t, fullManifest, ports.env())
	var out bytes.Buffer
	d := newDevRunner(&out)
	f := &fakeCommands{}
	f.install(d)
	d.database, d.services = true, true

	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	want := []string{
		"docker compose version",
		"docker compose ps --services --status running",
		"docker compose up -d --wait",
		"go run ./cmd/migrate",
		"go run ./cmd/seed",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("commands = %q, want %q", f.calls, want)
	}
	info, err := os.Stat(".env")
	if err != nil || info.Mode().Perm() != 0o600 || readFile(t, ".env") != ports.env() {
		t.Errorf(".env = %v %v, want a 0600 copy of .env.example", info, err)
	}
	for _, s := range []string{
		"orb: created .env from .env.example",
		"✓ API docs   http://127.0.0.1:" + ports.app + "/docs",
		"✓ Emails     caught at 127.0.0.1:1025", // MAIL_DELIVERY unset: orb dev's catcher (ADR-0074)
		"orb dev --observability",
	} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, out.String())
		}
	}
	if len(d.extraEnv) != 0 {
		t.Errorf("extraEnv = %q, want none without --observability", d.extraEnv)
	}

	// An existing .env is kept. It keeps the free ports, so the test passes
	// next to an app using the default ones.
	writeFile(t, ".env", ports.env()+"# edited\n")
	out.Reset()
	if err := d.prepare(context.Background()); err != nil || strings.Contains(out.String(), "created .env") || readFile(t, ".env") == ports.env() {
		t.Errorf("prepare() with .env = %v, output %q", err, out.String())
	}
}

func TestDevPrepareObservability(t *testing.T) {
	ports := newDevPorts(t)
	newDevApp(t, "preset: minimal\n", ports.env())
	writeFile(t, ".env", ports.env())
	var out bytes.Buffer
	d := newDevRunner(&out)
	f := &fakeCommands{}
	f.install(d)
	d.services, d.observability = true, true

	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	want := []string{
		"docker compose version",
		"docker compose --profile observability ps --services --status running",
		"docker compose --profile observability up -d --wait",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("commands = %q, want %q", f.calls, want)
	}
	if wantEnv := []string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:" + ports.otlp}; !slices.Equal(d.extraEnv, wantEnv) {
		t.Errorf("extraEnv = %q, want %q", d.extraEnv, wantEnv)
	}
	if s := out.String(); !strings.Contains(s, "✓ Grafana    http://127.0.0.1:"+ports.grafana) || strings.Contains(s, "Emails") || strings.Contains(s, "Tip:") {
		t.Errorf("output = %s", s)
	}
}

func TestDevPrepareMinimalAppNeedsNoDocker(t *testing.T) {
	ports := newDevPorts(t)
	dir := newDevApp(t, "preset: minimal\n", ports.env())
	var out bytes.Buffer
	d := newDevRunner(&out)
	f := &fakeCommands{noTool: true}
	f.install(d)
	d.services = true

	if err := d.prepare(context.Background()); err != nil || len(f.calls) != 0 {
		t.Fatalf("prepare() = %v, commands %q; want nothing run", err, f.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Error("prepare() created .env in an app without a database")
	}
}

func TestDevPrepareWithoutDocker(t *testing.T) {
	newDevApp(t, fullManifest, newDevPorts(t).env())
	d := newDevRunner(&bytes.Buffer{})
	f := &fakeCommands{noTool: true}
	f.install(d)
	d.database, d.services = true, true

	err := d.prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Docker isn't installed") || !strings.Contains(err.Error(), "--no-services") || len(f.calls) != 0 {
		t.Errorf("prepare() without Docker = %v, commands %q", err, f.calls)
	}

	f = &fakeCommands{fail: map[string]bool{"docker compose ps --services --status running": true}}
	f.install(d)
	if err := d.prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "Docker isn't running") {
		t.Errorf("prepare() with Docker stopped = %v", err)
	}
}

func TestDevPrepareNoServices(t *testing.T) {
	ports := newDevPorts(t)
	newDevApp(t, fullManifest, ports.env())
	var out bytes.Buffer
	d := newDevRunner(&out)
	f := &fakeCommands{noTool: true}
	f.install(d)
	d.database = true

	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if want := []string{"go run ./cmd/migrate", "go run ./cmd/seed"}; !slices.Equal(f.calls, want) {
		t.Errorf("commands = %q, want %q", f.calls, want)
	}
	if strings.Contains(out.String(), "http://127.0.0.1:"+ports.web) {
		t.Errorf("output lists Mailpit, which orb dev didn't start:\n%s", out.String())
	}
}

func TestDevPreparePortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, busy, _ := net.SplitHostPort(ln.Addr().String())
	ports := newDevPorts(t)
	ports.postgres = busy
	newDevApp(t, fullManifest, ports.env())
	d := newDevRunner(&bytes.Buffer{})
	f := &fakeCommands{}
	f.install(d)
	d.database, d.services = true, true

	err = d.prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "port "+busy+" for postgres") || !strings.Contains(err.Error(), "POSTGRES_PORT=") ||
		!strings.Contains(err.Error(), "DATABASE_URL") || slices.Contains(f.calls, "docker compose up -d --wait") {
		t.Errorf("prepare() with the database port taken = %v, commands %q", err, f.calls)
	}

	// The app's own running PostgreSQL holds the port: not a conflict.
	f = &fakeCommands{outputs: map[string]string{"docker compose ps --services --status running": "mailpit\npostgres\n"}}
	f.install(d)
	if err := d.prepare(context.Background()); err != nil {
		t.Errorf("prepare() with the app's own services running = %v", err)
	}
}

func TestDevPrepareStopsWhenMigrationsFail(t *testing.T) {
	newDevApp(t, fullManifest, newDevPorts(t).env())
	d := newDevRunner(&bytes.Buffer{})
	f := &fakeCommands{fail: map[string]bool{"go run ./cmd/migrate": true}}
	f.install(d)
	d.database, d.services = true, true

	if err := d.prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "migrations failed") || slices.Contains(f.calls, "go run ./cmd/seed") {
		t.Errorf("prepare() with failing migrations = %v, commands %q", err, f.calls)
	}
}

func TestManifestFeatures(t *testing.T) {
	for manifest, want := range map[string][]string{
		"name: x\nfeatures: [postgres, settings, jobs]\nmail: resend\n": {"postgres", "settings", "jobs"},
		"features:\n  - postgres\n  - auth\npreset: full\n":             {"postgres", "auth"},
		"features: []\n":    nil,
		"preset: minimal\n": nil,
	} {
		if got := manifestFeatures([]byte(manifest)); !slices.Equal(got, want) {
			t.Errorf("manifestFeatures(%q) = %q, want %q", manifest, got, want)
		}
	}
}

func TestSnapshotSeparatesMigrations(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "main.go", "package main\n")
	writeFile(t, filepath.Join("db", "migrations", "1_init.sql"), "-- +goose Up\n")
	all, _ := snapshot(".", watched)
	sql, _ := snapshot(migrationsDir, isSQL)

	later := time.Now().Add(time.Minute)
	if err := os.Chtimes("main.go", later, later); err != nil {
		t.Fatal(err)
	}
	if a, _ := snapshot(".", watched); a == all {
		t.Error("a Go change didn't change the app snapshot")
	}
	if s, _ := snapshot(migrationsDir, isSQL); s != sql {
		t.Error("a Go change changed the migrations snapshot")
	}
	writeFile(t, filepath.Join("db", "migrations", "2_more.sql"), "-- +goose Up\n")
	if s, _ := snapshot(migrationsDir, isSQL); s == sql {
		t.Error("a new migration didn't change the migrations snapshot")
	}
}

// TestDevSetsAppEnv checks that orb dev runs apps, which refuse to start
// without APP_ENV, in development unless the environment or .env says
// otherwise.
func TestDevSetsAppEnv(t *testing.T) {
	for _, tt := range []struct {
		env  []string
		want string
	}{
		{[]string{"PATH=/bin"}, "development"},
		{[]string{"APP_ENV="}, "development"},
		{[]string{"APP_ENV=production"}, "production"},
	} {
		if got := envValue(withAppEnv(tt.env), "APP_ENV", ""); got != tt.want {
			t.Errorf("withAppEnv(%q) APP_ENV = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestDevPrepareFreshEmptiesTheDatabaseFirst(t *testing.T) {
	newDevApp(t, fullManifest, newDevPorts(t).env()+"DATABASE_URL=postgres://app:app@localhost:5432/app?sslmode=disable\n")
	var out bytes.Buffer
	d := newDevRunner(&out)
	f := &fakeCommands{noTool: true}
	f.install(d)
	d.database, d.fresh = true, true
	d.dropSchema = func(context.Context) error {
		f.calls = append(f.calls, "drop schema public")
		return nil
	}

	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if want := []string{"drop schema public", "go run ./cmd/migrate", "go run ./cmd/seed"}; !slices.Equal(f.calls, want) {
		t.Errorf("commands = %q, want %q", f.calls, want)
	}
	if !strings.Contains(out.String(), "--fresh dropped every table") {
		t.Errorf("output doesn't say the database was emptied:\n%s", out.String())
	}
}

func TestDevPrepareFreshRefusesARemoteDatabase(t *testing.T) {
	newDevApp(t, fullManifest, newDevPorts(t).env()+"DATABASE_URL=postgres://app:secret@db.example.com:5432/app\n")
	d := newDevRunner(&bytes.Buffer{})
	f := &fakeCommands{noTool: true}
	f.install(d)
	d.database, d.fresh = true, true
	dropped := false
	d.dropSchema = func(context.Context) error { dropped = true; return nil }

	err := d.prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "db.example.com") || !strings.Contains(err.Error(), "only a local database") {
		t.Errorf("prepare() = %v, want a refusal naming the host", err)
	}
	if dropped || len(f.calls) != 0 {
		t.Errorf("a remote database was touched: dropped %v, commands %q", dropped, f.calls)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("the refusal prints the password: %v", err)
	}
}

func TestDevFreshNeedsADatabase(t *testing.T) {
	newDevApp(t, "preset: minimal\n", newDevPorts(t).env())
	err := runDev(context.Background(), []string{"--fresh"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "has none") {
		t.Errorf("orb dev --fresh in a Minimal app = %v", err)
	}
}

func TestLocalDatabase(t *testing.T) {
	for raw, want := range map[string]bool{
		"postgres://app:app@localhost:5432/app":               true,
		"postgres://app:app@LOCALHOST/app":                    true,
		"postgresql://app@127.0.0.1:5433/app?sslmode=disable": true,
		"postgres://app@[::1]:5432/app":                       true,
		"postgres:///app":                                     true,
		"postgres:///app?host=/var/run/postgresql":            true,
		"postgres:///app?host=db.example.com":                 false,
		"postgres://app@db.example.com/app":                   false,
		"postgres://app@10.0.0.5/app":                         false,
		"postgres://app@postgres:5432/app":                    false,
		"mysql://app@localhost/app":                           false,
		"":                                                    false,
	} {
		if got := localDatabase(raw); got != want {
			t.Errorf("localDatabase(%q) = %v, want %v", raw, got, want)
		}
	}
}
