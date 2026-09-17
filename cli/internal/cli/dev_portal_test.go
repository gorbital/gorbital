package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/portal"
	"gorbital.dev/cli/internal/routes"
)

// preparePortalApp runs prepare in a stand-in Minimal app with the portal
// on, returning the runner and its output.
func preparePortalApp(t *testing.T, env string) (*devRunner, string) {
	t.Helper()
	t.Setenv(devPortalTokenVar, "")
	newDevApp(t, "name: shop\nmodule: example.com/shop\npreset: minimal\n", env)
	writeFile(t, ".env", env)
	var out bytes.Buffer
	d := newDevRunner(&out)
	(&fakeCommands{noTool: true}).install(d)
	d.portal = true
	d.portalPortFlag = freePort(t)
	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	return d, out.String()
}

func TestDevPortalTokenPerRun(t *testing.T) {
	ports := newDevPorts(t)
	d, out := preparePortalApp(t, ports.env())
	if err := portal.CheckToken(d.portalToken); err != nil || d.portalTokenFromEnv {
		t.Fatalf("token %q: %v, fromEnv %v", d.portalToken, err, d.portalTokenFromEnv)
	}
	link := "http://127.0.0.1:" + d.portalPort + portal.AuthPath + "?t=" + d.portalToken
	if !strings.Contains(out, "✓ Dev Portal "+link) {
		t.Errorf("output lacks the portal link %q:\n%s", link, out)
	}
	if strings.Contains(out, "from your environment") {
		t.Error("a generated token is reported as from the environment")
	}
	// Never on disk.
	dir, _ := os.Getwd()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if data, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && bytes.Contains(data, []byte(d.portalToken)) {
			t.Errorf("%s holds the portal token", e.Name())
		}
	}
	again, _ := preparePortalApp(t, ports.env())
	if again.portalToken == d.portalToken {
		t.Error("the second run reused the token")
	}
}

func TestDevPortalTokenAndPortFromEnvironment(t *testing.T) {
	ports := newDevPorts(t)
	port := freePort(t)
	newDevApp(t, "preset: minimal\n", ports.env())
	writeFile(t, ".env", ports.env()+devPortalPortVar+"="+port+"\n")
	t.Setenv(devPortalTokenVar, strings.Repeat("t", 40))
	var out bytes.Buffer
	d := newDevRunner(&out)
	(&fakeCommands{noTool: true}).install(d)
	d.portal = true
	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if d.portalPort != port || d.portalToken != strings.Repeat("t", 40) || !d.portalTokenFromEnv {
		t.Errorf("port %q token %q fromEnv %v", d.portalPort, d.portalToken, d.portalTokenFromEnv)
	}
	if !strings.Contains(out.String(), "Token      "+devPortalTokenVar+" from your environment") {
		t.Errorf("output = %s", out.String())
	}

	t.Setenv(devPortalTokenVar, "short")
	d = newDevRunner(&out)
	(&fakeCommands{noTool: true}).install(d)
	d.portal = true
	if err := d.prepare(context.Background()); err == nil || !strings.Contains(err.Error(), devPortalTokenVar) {
		t.Errorf("prepare() with a short token = %v", err)
	}
}

func TestDevPortalPortInUse(t *testing.T) {
	ports := newDevPorts(t)
	newDevApp(t, "preset: minimal\n", ports.env())
	writeFile(t, ".env", ports.env())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	d := newDevRunner(&bytes.Buffer{})
	(&fakeCommands{noTool: true}).install(d)
	d.portal, d.portalPortFlag = true, port
	err = d.prepare(context.Background())
	if err == nil || !strings.Contains(err.Error(), "port "+port+" for the Dev Portal") || !strings.Contains(err.Error(), "--no-portal") {
		t.Errorf("prepare() with the portal port taken = %v", err)
	}
	d.portalPortFlag = "nope"
	if err := d.prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "port number") {
		t.Errorf("prepare() with a bad port = %v", err)
	}
}

func TestDevPortalOffByFlag(t *testing.T) {
	ports := newDevPorts(t)
	newDevApp(t, "preset: minimal\n", ports.env())
	var out bytes.Buffer
	d := newDevRunner(&out)
	(&fakeCommands{noTool: true}).install(d)
	if err := d.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Dev Portal") || d.portalToken != "" {
		t.Errorf("portal prepared while off: %s", out.String())
	}
	stop, err := d.servePortal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stop()
}

// portalClient calls the portal served by d.
func portalClient(t *testing.T, d *devRunner, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, d.portalURL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.portalToken)
	if method != http.MethodGet {
		req.Header.Set(portal.MutationHeader, "1")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestDevPortalServesStatusAndOutput(t *testing.T) {
	ports := newDevPorts(t)
	d, _ := preparePortalApp(t, ports.env())
	opened := ""
	d.open = func(url string) error { opened = url; return nil }
	d.openBrowser = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := d.servePortal(ctx)
	if err != nil {
		t.Fatalf("servePortal() error = %v", err)
	}
	defer stop()
	// Output isn't a terminal here, so no browser opens.
	if opened != "" {
		t.Errorf("opened %q without a terminal", opened)
	}

	res := portalClient(t, d, http.MethodGet, portal.APIPrefix+"status", "")
	var status portal.Status
	if err := json.NewDecoder(res.Body).Decode(&status); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, %v", res.StatusCode, err)
	}
	if status.Project.Name != "shop" || status.Project.Module != "example.com/shop" || status.Project.Preset != "minimal" || status.Project.Database ||
		status.App.State != portal.StatePreparing || status.App.Addr != "127.0.0.1:"+ports.app || status.App.URL != "http://127.0.0.1:"+ports.app ||
		status.Links["api"] != "http://127.0.0.1:"+ports.app || status.Links["docs"] != "http://127.0.0.1:"+ports.app+"/docs" ||
		status.Portal.Version != Version || (status.Portal.UI != "placeholder" && status.Portal.UI != "bundled") || strings.Join(status.Generators, ",") != "add-mail,add-orgs,add-rls,add-storage,job,middleware,migration,module,resource" {
		t.Errorf("status = %+v", status)
	}
	if _, ok := status.Links["mail"]; ok {
		t.Error("a Minimal app has a mail link")
	}

	// orb's own messages reach the portal's output.
	res = portalClient(t, d, http.MethodGet, portal.APIPrefix+"output", "")
	var out portal.OutputList
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range out.Lines {
		if line.Stream == "orb" && strings.Contains(line.Text, "✓ API docs") {
			found = true
		}
	}
	if !found {
		t.Errorf("output lacks orb's banner: %+v", out.Lines)
	}

	// Actions queue commands for loop; a second one waits its turn.
	res = portalClient(t, d, http.MethodPost, portal.APIPrefix+"app/restart", "")
	if res.StatusCode != http.StatusAccepted {
		t.Errorf("restart = %d", res.StatusCode)
	}
	res = portalClient(t, d, http.MethodPost, portal.APIPrefix+"app/stop", "")
	if res.StatusCode != http.StatusConflict {
		t.Errorf("second action = %d, want 409 while the first waits", res.StatusCode)
	}
	if c := <-d.commands; c != commandRestart {
		t.Errorf("queued command = %q", c)
	}

	// Without the token, nothing.
	req, _ := http.NewRequest(http.MethodGet, d.portalURL()+portal.APIPrefix+"status", nil)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status without token = %v %v", res, err)
	}
	// The UI (a placeholder in a checkout without the synced export) is served at /.
	if res, err := http.Get(d.portalURL() + "/"); err != nil || res.StatusCode != http.StatusOK {
		t.Errorf("GET / = %v %v", res, err)
	}
}

func TestDevPortalGeneratorsMatchTheCLI(t *testing.T) {
	ports := newDevPorts(t)
	t.Setenv(devPortalTokenVar, "")
	dir := newFullApp(t)
	writeFile(t, filepath.Join(dir, "gorbital.yaml"), "name: shop\nmodule: example.com/shop\npreset: full\nfeatures: [postgres]\n")
	writeFile(t, filepath.Join(dir, ".env.example"), ports.env())
	writeFile(t, filepath.Join(dir, ".env"), ports.env())
	writeFile(t, filepath.Join(dir, "db", "migrations", ".keep"), "")
	var out bytes.Buffer
	d := newDevRunner(&out)
	(&fakeCommands{noTool: true}).install(d)
	d.portal, d.portalPortFlag = true, freePort(t)
	if err := d.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	stop, err := d.servePortal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// The CLI's dry run and the portal's plan describe the same files.
	code, cliOut, errOut := runOrb(t, "gen", "job", "CleanupSessions", "--schedule", "30 2 * * *", "--timeout", "5m", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("orb gen job = %d %s", code, errOut)
	}
	var cliResult genJobResult
	if err := json.Unmarshal([]byte(cliOut), &cliResult); err != nil {
		t.Fatal(err)
	}
	res := portalClient(t, d, http.MethodPost, portal.APIPrefix+"generators/job/plan", `{"input":{"name":"CleanupSessions","schedule":"30 2 * * *","timeout":"5m"}}`)
	var planned portal.GeneratorResponse
	if err := json.NewDecoder(res.Body).Decode(&planned); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("plan = %d %v", res.StatusCode, err)
	}
	if planned.Applied || strings.Join(planned.Plan.Paths(), ",") != strings.Join(cliResult.Files, ",") {
		t.Errorf("portal plan %q, CLI %q", planned.Plan.Paths(), cliResult.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "app", "job_cleanup_sessions.go")); err == nil {
		t.Error("plan wrote files")
	}
	var jobsGo genplan.Change
	for _, c := range planned.Plan.Changes {
		if c.Path == "internal/app/jobs.go" {
			jobsGo = c
		}
	}
	if jobsGo.Kind != genplan.Modify || string(jobsGo.Before) != testJobsGo || !strings.Contains(string(jobsGo.Content), "defineCleanupSessionsJob(defs, deps)") {
		t.Errorf("jobs.go change = %+v", jobsGo)
	}

	// Unknown fields and missing names are refused before anything runs.
	res = portalClient(t, d, http.MethodPost, portal.APIPrefix+"generators/job/plan", `{"input":{"name":"X","colour":"red"}}`)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown field = %d", res.StatusCode)
	}
	res = portalClient(t, d, http.MethodPost, portal.APIPrefix+"generators/migration/plan", `{"input":{}}`)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("missing name = %d", res.StatusCode)
	}

	// Apply writes what the plan showed. The stand-in app isn't a git
	// repository, so the clean-tree check passes.
	res = portalClient(t, d, http.MethodPost, portal.APIPrefix+"generators/migration/apply", `{"input":{"name":"add_phone"}}`)
	var applied portal.GeneratorResponse
	if err := json.NewDecoder(res.Body).Decode(&applied); err != nil || res.StatusCode != http.StatusOK || !applied.Applied {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("apply = %d %v %s", res.StatusCode, err, body)
	}
	written := readFile(t, filepath.Join(dir, filepath.FromSlash(applied.Plan.Changes[0].Path)))
	if !strings.HasSuffix(applied.Plan.Changes[0].Path, "_add_phone.sql") || !strings.Contains(written, "-- +goose Up") {
		t.Errorf("applied %s = %q", applied.Plan.Changes[0].Path, written)
	}
}

func TestDevRunnerStatusFollowsTheProcess(t *testing.T) {
	d := newDevRunner(&bytes.Buffer{})
	sub := d.hub.Subscribe()
	defer d.hub.Unsubscribe(sub)
	if s := d.Status(); s.State != portal.StatePreparing || s.PID != 0 || s.StartedAt != nil {
		t.Errorf("initial status = %+v", s)
	}
	d.setState(portal.StateBuilding, "")
	d.setStateAfterFailure("build failed: boom")
	if s := d.Status(); s.State != portal.StateStopped || s.Problem != "build failed: boom" {
		t.Errorf("after a failed build with no process = %+v", s)
	}
	d.exited(nil)
	if s := d.Status(); s.State != portal.StateStopped || s.Problem != "" {
		t.Errorf("after a clean exit = %+v", s)
	}
	if len(sub.C) != 3 {
		t.Errorf("state events = %d, want 3", len(sub.C))
	}
	for addr, want := range map[string]string{"0.0.0.0:8080": "127.0.0.1:8080", "[::]:8080": "127.0.0.1:8080", "localhost:1": "localhost:1", "bad": "bad"} {
		if got := reachableAddr(addr); got != want {
			t.Errorf("reachableAddr(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestPortalJobInputDefaults(t *testing.T) {
	app := func() (appInfo, error) { return appInfo{dir: ".", module: "example.com/shop"}, nil }
	_, in, err := portalJobInput(app, json.RawMessage(`{"name":"SendDigest"}`))
	if err != nil || in.trigger != triggerSchedule || in.schedule != "0 3 * * *" || in.timeout != "1m" || in.maxAttempts != 5 || in.queue != "default" || in.priority != 1 || !in.enabled {
		t.Errorf("defaults = %+v, %v", in, err)
	}
	_, in, err = portalJobInput(app, json.RawMessage(`{"name":"SendDigest","every":"6h","disabled":true}`))
	if err != nil || in.trigger != triggerInterval || in.every != "6h" || in.enabled {
		t.Errorf("interval = %+v, %v", in, err)
	}
	_, in, err = portalJobInput(app, json.RawMessage(`{"name":"SendDigest","trigger":"manual"}`))
	if err != nil || in.trigger != triggerManual {
		t.Errorf("manual = %+v, %v", in, err)
	}
	if _, _, err := portalJobInput(app, json.RawMessage(`{"name":"X","trigger":"sometimes"}`)); err == nil {
		t.Error("unknown trigger accepted")
	}
	if _, _, err := portalJobInput(app, json.RawMessage(`{"name":""}`)); err == nil {
		t.Error("empty name accepted")
	}
	if _, _, err := portalJobInput(app, json.RawMessage(`not json`)); err == nil {
		t.Error("bad JSON accepted")
	}
}

// TestPortalModuleAndMiddlewareGenerators plans and applies the Phase 8
// generators as the portal's hub does, with the CLI's plans.
func TestPortalModuleAndMiddlewareGenerators(t *testing.T) {
	dir := newMainApp(t, false)
	d := newDevRunner(&bytes.Buffer{})
	d.dir = dir
	gens := d.generators()
	ctx := context.Background()

	plan, err := gens["module"].Plan(ctx, json.RawMessage(`{"name":"Shelf","fields":["name:string:unique","description:text","visibility:enum(private,shared)"],"plural":"Shelves"}`))
	if err != nil || plan.Generator != "module" || len(plan.Changes) != 27 || plan.Result.(genModuleResult).Route != "/v1/shelves" {
		t.Fatalf("module plan = %+v, %v", plan, err)
	}
	if _, err := gens["module"].Plan(ctx, json.RawMessage(`{"name":"Shelf","fields":["name:string"],"org":true}`)); err == nil || !strings.Contains(err.Error(), "Phase 7") {
		t.Errorf("module plan with org = %v", err)
	}
	if _, err := gens["module"].Apply(ctx, json.RawMessage(`{"name":"Shelf","fields":["name:string:unique","description:text","visibility:enum(private,shared)"],"plural":"Shelves"}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "modules", "shelves", "delivery", "routes.go")); err != nil {
		t.Errorf("module not applied: %v", err)
	}

	plan, err = gens["middleware"].Plan(ctx, json.RawMessage(`{"name":"RequireClientVersion","module":"shelves"}`))
	if err != nil || plan.Result.(genMiddlewareResult).Wire != "gorbital.Use(RequireClientVersion)" {
		t.Fatalf("middleware plan = %+v, %v", plan, err)
	}
	for input, want := range map[string]string{
		`{"module":"shelves"}`:                    "missing middleware name",
		`{"name":"X","global":true,"guard":true}`: "--guard needs --module",
		`{"name":"X"}`:                            "--module <name>",
	} {
		if _, err := gens["middleware"].Plan(ctx, json.RawMessage(input)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("middleware plan %s = %v, want %q", input, err, want)
		}
	}
	if _, err := gens["middleware"].Apply(ctx, json.RawMessage(`{"name":"TenantHeader","global":true}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "middleware", "tenant_header.go")); err != nil {
		t.Errorf("middleware not applied: %v", err)
	}

	// Routes: the app isn't running, so they come from the export.
	fakeRoutesExport(t, nil)
	// Every route of the document, the library modules' too; the app's own
	// are the books and the new shelves routes, with their source.
	doc, _, err := routes.FromOpenAPI([]byte(readFile(t, filepath.Join(dir, "api", "openapi.json"))))
	if err != nil {
		t.Fatal(err)
	}
	list, err := d.routes(ctx)
	if err != nil || list.Source != "export" || list.Total != len(doc) {
		t.Errorf("routes = %+v, %v", list, err)
	}
	app := list.Filter("", false, true)
	if app.Total != 10 || app.Filter("shelves", false, false).Total != 5 {
		t.Errorf("the app's routes = %+v", app)
	}
}
