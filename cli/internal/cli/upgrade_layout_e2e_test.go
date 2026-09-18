package cli

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// compatTool is a small program the layout test writes into an app: it
// compares the app's OpenAPI document with the one it had before the move,
// with the library's own compatibility check (ADR-0054).
const compatTool = `package main

import (
	"fmt"
	"os"

	"gorbital.dev/modules/openapi"
)

func main() {
	baseline, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	current, err := os.ReadFile(os.Args[2])
	if err != nil {
		panic(err)
	}
	problems, err := openapi.CheckCompatible(baseline, current, "")
	if err != nil {
		panic(err)
	}
	for _, p := range problems {
		fmt.Println(p.String())
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}
`

// TestLayoutMoveOnV01Apps moves apps of the v0.1 layout to the v0.2 layout
// and proves each one afterwards: it builds, its tests pass, its OpenAPI
// document stays compatible with the one it had, orb doctor is happy, and a
// database the v0.1 app migrated has nothing left to apply (roadmap item
// 87). The apps come from the published orb v0.1.0 when the module cache or
// the network has it, and otherwise from this orb's v0.1 templates, which
// are that release's byte for byte.
//
// Set ORB_E2E=1 and GORBITAL_TEST_DATABASE_URL to run it.
func TestLayoutMoveOnV01Apps(t *testing.T) {
	adminURL := os.Getenv("GORBITAL_TEST_DATABASE_URL")
	if os.Getenv("ORB_E2E") == "" || adminURL == "" {
		t.Skip("set ORB_E2E=1 and GORBITAL_TEST_DATABASE_URL to run the end-to-end test")
	}
	repo, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	isolateGit(t)
	published := publishedOrb(t)

	t.Run("untouched single-tenant app", func(t *testing.T) {
		app := newV01E2EApp(t, repo, published, "movesingle", "single")
		app.convert(t)
		app.check(t, adminURL)
	})

	t.Run("untouched multi-tenant app", func(t *testing.T) {
		app := newV01E2EApp(t, repo, published, "movemulti", "multi")
		app.convert(t)
		app.check(t, adminURL)
	})

	t.Run("customised sign-in", func(t *testing.T) {
		app := newV01E2EApp(t, repo, published, "moveauth", "single")
		// A rule the app added to the generated sign-in module, which no
		// option or hook of the library covers.
		register := filepath.Join(app.dir, "internal", "modules", "auth", "usecase", "register.go")
		src := readFile(t, register)
		src = strings.Replace(src, "\t\"errors\"\n\t\"time\"\n", "\t\"errors\"\n\t\"strings\"\n\t\"time\"\n", 1)
		src = strings.Replace(src, "\tdefer s.padResponse(ctx, time.Now())",
			"\t// Addresses of domains we ban never get an account (a rule of ours).\n"+
				"\tif strings.HasSuffix(normalized, \"@blocked.example\") {\n\t\treturn authlib.ErrInvalidEmail\n\t}\n\tdefer s.padResponse(ctx, time.Now())", 1)
		if !strings.Contains(src, "blocked.example") {
			t.Fatal("the generated sign-in module changed; customise another line")
		}
		writeFile(t, register, src)
		app.commit(t, "Ban registrations from blocked.example")

		res := app.convert(t)
		if len(res.Ejected) != 1 || res.Ejected[0].Module != "auth" || len(res.Ejected[0].Carried) == 0 {
			t.Fatalf("the changed sign-in module isn't kept as the app's code: %+v", res.Ejected)
		}
		if item, ok := itemFor(res, "internal/modules/auth"); !ok || item.Kind != itemKept || len(item.Hooks) == 0 {
			t.Errorf("internal/modules/auth: %+v, want a %s line naming the hooks it could use", item, itemKept)
		}
		kept := readFile(t, register)
		if !strings.Contains(kept, "blocked.example") || !strings.Contains(kept, "RegisterWithFields") {
			t.Errorf("the change isn't in the library's module:\n%s", kept)
		}
		lock, err := readLock(app.dir)
		if err != nil || len(lock.Ejected) != 1 || lock.Ejected[0].Package != gorbitalImportPath+"/authhttp" {
			t.Errorf("gorbital.lock records %+v, %v", lock.Ejected, err)
		}
		app.check(t, adminURL)
	})

	t.Run("middleware of its own", func(t *testing.T) {
		app := newV01E2EApp(t, repo, published, "movestack", "single")
		writeFile(t, filepath.Join(app.dir, "internal", "app", "client_version.go"), clientVersionMiddleware)
		routes := filepath.Join(app.dir, "internal", "app", "routes.go")
		src := readFile(t, routes)
		src = strings.Replace(src, "\t\ta.maintenance(), //", "\t\trequireClientVersion, // 426 for clients older than minClientVersion (client_version.go)\n\t\ta.maintenance(), //", 1)
		if !strings.Contains(src, "requireClientVersion") {
			t.Fatal("the generated routes.go changed; add the middleware another way")
		}
		writeFile(t, routes, src)
		app.commit(t, "Refuse old mobile clients")

		res := app.convert(t)
		if res.Middleware == nil || res.Middleware.Option != "gorbital.WithStack(stack)" || len(res.Middleware.Moved) != 1 {
			t.Fatalf("the app's middleware isn't carried over: %+v", res.Middleware)
		}
		stack := readFile(t, filepath.Join(app.dir, "cmd", "api", "stack.go"))
		// It runs where it ran: after the body limit, before the
		// maintenance switch and authentication.
		at := strings.Index(stack, "requireClientVersion")
		if at < 0 || at < strings.Index(stack, "s.BodyLimit") || at > strings.Index(stack, "s.Maintenance") {
			t.Errorf("cmd/api/stack.go doesn't run the middleware where routes.go did:\n%s", stack)
		}
		if main := readFile(t, filepath.Join(app.dir, "cmd", "api", "main.go")); !strings.Contains(main, "gorbital.WithStack(stack)") {
			t.Errorf("cmd/api/main.go doesn't use the stack:\n%s", main)
		}
		app.check(t, adminURL)
	})

	t.Run("a generated resource and a module written by hand", func(t *testing.T) {
		app := newV01E2EApp(t, repo, published, "movemodules", "single")
		app.run(t, app.orb, "gen", "resource", "Note", "title:string:unique", "body:text", "--no-input")
		writeHandwrittenModule(t, app.dir, app.name)
		app.run(t, "go", "run", "./cmd/api", "openapi", "--dir", "api")
		app.commit(t, "Add the notes resource and a status module")
		app.saveDocument(t) // the API to stay compatible with

		res := app.convert(t)
		for _, name := range []string{"notes", "projects", "status"} {
			if !hasModule(res, name) {
				t.Errorf("the %s module wasn't converted: %+v", name, res.Modules)
			}
		}
		modules := readFile(t, filepath.Join(app.dir, "internal", "modules", "modules.gen.go"))
		for _, want := range []string{"notes.Module()", "status.Module()"} {
			if !strings.Contains(modules, want) {
				t.Errorf("modules.gen.go doesn't list %s:\n%s", want, modules)
			}
		}
		document := readFile(t, filepath.Join(app.dir, "api", "openapi.json"))
		for _, want := range []string{`"/v1/notes"`, `"/v1/status"`} {
			if !strings.Contains(document, want) {
				t.Errorf("api/openapi.json doesn't route %s", want)
			}
		}
		app.check(t, adminURL)
	})
}

// clientVersionMiddleware is middleware an app added to its v0.1 stack.
const clientVersionMiddleware = `package app

import (
	"net/http"
	"strings"

	"gorbital.dev/httpx"
)

// minClientVersion is the oldest mobile client the API answers.
const minClientVersion = "2.0"

// requireClientVersion refuses requests from older mobile clients.
func requireClientVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if version := r.Header.Get("X-Client-Version"); version != "" && strings.Compare(version, minClientVersion) < 0 {
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUpgradeRequired, "client_too_old", "update the app to version "+minClientVersion+" or newer"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
`

// writeHandwrittenModule writes a small module of the app's own, in the v0.1
// layout: layered code with huma operations, registered from internal/app.
func writeHandwrittenModule(t *testing.T, dir, module string) {
	t.Helper()
	files := map[string]string{
		"internal/modules/status/domain/status.go": `// Package domain holds the status module's rules.
package domain

import "errors"

// Status is what the status endpoint answers.
type Status struct{ Region string }

// ErrUnavailable is returned while the instance is starting.
var ErrUnavailable = errors.New("status: unavailable")
`,
		"internal/modules/status/usecase/service.go": `// Package usecase holds the status module's application logic.
package usecase

import (
	"context"

	"MODULE/internal/modules/status/domain"
)

// Service answers where this instance runs.
type Service struct{ region string }

// NewService returns a Service reporting region.
func NewService(region string) *Service { return &Service{region: region} }

// Status reports the instance's region.
func (s *Service) Status(context.Context) (domain.Status, error) {
	return domain.Status{Region: s.region}, nil
}
`,
		"internal/modules/status/delivery/status.go": `// Package delivery is the status module's HTTP adapter.
package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"MODULE/internal/modules/status/usecase"
)

type statusOutput struct {
	Body struct {
		Region string ` + "`json:\"region\"`" + `
	}
}

type handler struct{ svc *usecase.Service }

// Register adds the status operations to api.
func Register(api huma.API, svc *usecase.Service) {
	h := &handler{svc: svc}
	huma.Register(api, huma.Operation{
		OperationID: "status-get", Method: http.MethodGet, Path: "/v1/status",
		Summary: "Where this instance runs", Tags: []string{"Status"},
	}, h.get)
}

func (h *handler) get(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	s, err := h.svc.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := &statusOutput{}
	out.Body.Region = s.Region
	return out, nil
}
`,
		"internal/modules/status/module.go": `// Package status is a module written by hand: one public endpoint.
package status

import (
	"github.com/danielgtaylor/huma/v2"

	statusdelivery "MODULE/internal/modules/status/delivery"
	statususecase "MODULE/internal/modules/status/usecase"
)

// Module is the status module.
type Module struct{ svc *statususecase.Service }

// New builds the module for region.
func New(region string) *Module { return &Module{svc: statususecase.NewService(region)} }

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	var svc *statususecase.Service
	if m != nil {
		svc = m.svc
	}
	statusdelivery.Register(api, svc)
}
`,
		"internal/app/module_status.go": `package app

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/httpx"

	statusmodule "MODULE/internal/modules/status"
	statusdomain "MODULE/internal/modules/status/domain"
)

// registerStatus wires the status module.
func registerStatus(api huma.API, mapper *httpx.Mapper, svc services) error {
	if err := mapper.Add(
		httpx.Mapping{Err: statusdomain.ErrUnavailable, Status: http.StatusServiceUnavailable, Code: "status_unavailable", Detail: "the instance is starting"},
	); err != nil {
		return err
	}
	statusmodule.New("eu-west-1").Register(api)
	return nil
}
`,
	}
	for p, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), strings.ReplaceAll(content, "MODULE", module))
	}
	modules := filepath.Join(dir, "internal", "app", "modules.go")
	src := readFile(t, modules)
	src = strings.Replace(src, "\t\t//orb:anchor modules\n", "\t\t//orb:anchor modules\n\t\tregisterStatus(api, mapper, svc),\n", 1)
	if !strings.Contains(src, "registerStatus") {
		t.Fatal("internal/app/modules.go has no module anchor")
	}
	writeFile(t, modules, src)
}

func hasModule(res layoutResult, name string) bool {
	for _, c := range res.Modules {
		if c.Name == name {
			return true
		}
	}
	return false
}

// publishedOrb installs orb v0.1.0 from the module proxy, or returns "" when
// it isn't reachable, so the test runs without network on this orb's v0.1
// templates instead (they are that release's).
func publishedOrb(t *testing.T) string {
	t.Helper()
	version := os.Getenv("ORB_LAYOUT_FROM")
	if version == "" {
		version = "v0.1.0"
	}
	work := t.TempDir()
	bin := filepath.Join(work, "bin")
	install := exec.Command("go", "install", "gorbital.dev/cli/cmd/orb@"+version)
	install.Dir, install.Env = work, append(os.Environ(), "GOBIN="+bin, "GOWORK=off", "GOFLAGS=")
	if out, err := install.CombinedOutput(); err != nil {
		t.Logf("orb %s isn't installable (%v), so the apps come from this orb's v0.1 templates:\n%s", version, err, out)
		return ""
	}
	return filepath.Join(bin, "orb")
}

// v01App is an app of the v0.1 layout the test moves to the v0.2 layout.
type v01App struct {
	name, dir, orb, before string
	// database is a database the app migrated before the move, with the
	// v0.1 app's own migration files.
	database string
}

// newV01E2EApp creates a Full app of the v0.1 layout, wired to the library
// in this checkout, and commits it.
func newV01E2EApp(t *testing.T, repo, published, name, tenancy string) *v01App {
	t.Helper()
	work := t.TempDir()
	app := &v01App{name: name, dir: filepath.Join(work, name), orb: published}
	if published == "" {
		writeV01App(t, work, name, tenancy, repo)
		app.orb = "orb" // the fallback path uses this orb through runOrb
	} else {
		create := exec.Command(published, "new", name, "--preset", "full", "--tenancy", tenancy, "--no-git", "--skip-tidy", "--no-input", "--no-start")
		create.Dir = work
		if out, err := create.CombinedOutput(); err != nil {
			t.Fatalf("orb new: %v\n%s", err, out)
		}
		app.run(t, "go", append([]string{"mod", "edit"}, libraryReplaceArgs(t, repo)...)...)
	}
	app.run(t, "go", "mod", "tidy")
	t.Chdir(app.dir)
	commitAll(t, "Create "+name)
	app.saveDocument(t)
	return app
}

// libraryReplaceArgs points every library module, gorbital.dev/gorbital
// included, at the checkout.
func libraryReplaceArgs(t *testing.T, repo string) []string {
	t.Helper()
	args := libraryReplaces(t, repo)
	return append(args, "-replace=gorbital.dev/gorbital="+filepath.Join(repo, "gorbital"))
}

func (a *v01App) run(t *testing.T, name string, args ...string) string {
	t.Helper()
	return a.runEnv(t, nil, name, args...)
}

func (a *v01App) runEnv(t *testing.T, env []string, name string, args ...string) string {
	t.Helper()
	if name == "orb" {
		code, out, errOut := runOrbIn(t, a.dir, args...)
		if code != 0 {
			t.Fatalf("orb %s = %d; stdout %s stderr %s", strings.Join(args, " "), code, out, errOut)
		}
		return out
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = a.dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// runOrbIn runs this orb in dir.
func runOrbIn(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	t.Chdir(dir)
	return runOrb(t, args...)
}

func (a *v01App) commit(t *testing.T, message string) {
	t.Helper()
	t.Chdir(a.dir)
	commitAll(t, message)
}

// upgrade brings the app onto this release's v0.1 templates and commits the
// result, which orb upgrade --layout v0.2 requires. It is a no-op for an app
// already on them, and the branch orb upgrade leaves behind is merged back so
// the layout move sees a clean tree on the app's own branch.
func (a *v01App) upgrade(t *testing.T) {
	t.Helper()
	t.Chdir(a.dir)
	branch := git(t, "rev-parse", "--abbrev-ref", "HEAD")
	code, out, errOut := runOrb(t, "upgrade", "--json")
	if code != 0 {
		t.Fatalf("orb upgrade = %d; stdout %s stderr %s", code, out, errOut)
	}
	if on := git(t, "rev-parse", "--abbrev-ref", "HEAD"); on != branch {
		git(t, "checkout", "--quiet", branch)
		git(t, "merge", "--quiet", "--no-edit", on)
	}
}

// saveDocument keeps the app's OpenAPI document, to compare with after the
// move.
func (a *v01App) saveDocument(t *testing.T) {
	t.Helper()
	a.before = filepath.Join(t.TempDir(), "openapi.json")
	writeFile(t, a.before, readFile(t, filepath.Join(a.dir, "api", "openapi.json")))
}

// convert migrates a database with the app's v0.1 code, so the move can be
// proven against it, and runs orb upgrade --layout v0.2 in the app.
func (a *v01App) convert(t *testing.T) layoutResult {
	t.Helper()
	a.database = a.migratedDatabase(t, os.Getenv("GORBITAL_TEST_DATABASE_URL"))
	t.Chdir(a.dir)
	// The documented order: orb upgrade brings the app onto this release's
	// v0.1 templates and commits, and only then does the layout move run
	// (docs/guides/upgrade-notes.md). The move refuses an app that doesn't
	// match them, so a release that changed a v0.1 template needs this step.
	a.upgrade(t)
	code, out, errOut := runOrb(t, "upgrade", "--layout", "v0.2", "--yes", "--json")
	if code != 0 {
		t.Fatalf("orb upgrade --layout v0.2 = %d; stdout %s stderr %s", code, out, errOut)
	}
	var res layoutResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("orb upgrade --layout v0.2 --json output %q: %v", out, err)
	}
	if res.Blocked || len(res.Manual) > 0 {
		t.Fatalf("the move left changes for the developer: %v", res.Manual)
	}
	if !res.Built || !res.Tidied || !res.Surface || !res.Exported {
		t.Fatalf("the move didn't finish: %+v", res)
	}
	return res
}

// check proves the converted app: it is formatted, vets, passes its tests,
// keeps its API, satisfies orb doctor, and has nothing to migrate in a
// database the v0.1 app had already migrated.
func (a *v01App) check(t *testing.T, adminURL string) {
	t.Helper()
	if out := a.run(t, "gofmt", "-l", "."); strings.TrimSpace(out) != "" {
		t.Errorf("not gofmt-formatted after the move:\n%s", out)
	}
	a.run(t, "go", "vet", "./...")

	// The API the app publishes stays compatible: guards are added to the
	// document, nothing is removed.
	tool := filepath.Join(a.dir, "internal", "compattool", "main.go")
	writeFile(t, tool, compatTool)
	out := a.run(t, "go", "run", "./internal/compattool", a.before, filepath.Join(a.dir, "api", "openapi.json"))
	if strings.TrimSpace(out) != "" {
		t.Errorf("the API changed incompatibly:\n%s", out)
	}
	if err := os.RemoveAll(filepath.Dir(tool)); err != nil {
		t.Fatal(err)
	}

	code, doctorOut, errOut := runOrbIn(t, a.dir, "doctor", "--fast", "--json")
	var doctor doctorResult
	if err := json.Unmarshal([]byte(doctorOut), &doctor); err != nil || code != 0 {
		t.Fatalf("orb doctor = %d %s %s", code, doctorOut, errOut)
	}
	for _, c := range doctor.Checks {
		if c.Status == doctorFail {
			t.Errorf("orb doctor: %s: %s", c.Name, c.Detail)
		}
	}

	// A database the app migrated before the move has nothing to apply
	// after it: the library declares the same versions.
	status := a.runEnv(t, []string{"DATABASE_URL=" + a.database}, "go", "run", "./cmd/api", "migrate", "--status", "--json")
	var migrate struct {
		Pending int `json:"pending"`
	}
	if err := json.Unmarshal([]byte(status), &migrate); err != nil {
		t.Fatalf("migrate --status --json: %v\n%s", err, status)
	}
	if migrate.Pending != 0 {
		t.Errorf("a database of the v0.1 app has %d migrations to apply after the move:\n%s", migrate.Pending, status)
	}

	env := append(os.Environ(), "GORBITAL_TEST_DATABASE_URL="+adminURL)
	test := exec.Command("go", "test", "./...")
	test.Dir, test.Env = a.dir, env
	if out, err := test.CombinedOutput(); err != nil {
		t.Errorf("the converted app's tests fail: %v\n%s", err, out)
	}
}

// migratedDatabase creates a database, migrates it with the app's v0.1
// migration files, and returns its URL.
func (a *v01App) migratedDatabase(t *testing.T, adminURL string) string {
	t.Helper()
	tool := filepath.Join(a.dir, "internal", "schematool")
	writeFile(t, filepath.Join(tool, "main.go"), strings.Replace(schemaTool, "MODULE", a.name, 1))
	a.run(t, "go", "run", "./internal/schematool", "createdb", adminURL, a.name+"_v01")
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + a.name + "_v01"
	a.run(t, "go", "run", "./internal/schematool", "migrate", u.String())
	// The move refuses a repository with uncommitted changes, so the tool
	// goes again before it runs.
	if err := os.RemoveAll(tool); err != nil {
		t.Fatal(err)
	}
	return u.String()
}
