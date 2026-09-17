package opshttp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/observability"
)

// These tests are a v0.1 golden app's internal/app/observability_test.go,
// run against the library module.

// obsWaitOverview polls GET /ops/observability/overview with query until
// done accepts the answer, and returns it. The golden tests flushed the
// request collector by hand; an app's collector is internal, so the tests
// run the app (StartWorkers) and wait for its periodic write, every 15
// seconds.
func obsWaitOverview(t *testing.T, h http.Handler, query string, headers []string, done func(response) bool) response {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		r := do(t, h, "GET", "/ops/observability/overview"+query, "", headers...)
		if r.Code == http.StatusOK && done(r) {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the requests to be written: GET /ops/observability/overview%s = %d %s", query, r.Code, r.Body)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func obsFindRoute(list any, method, route string) map[string]any {
	items, _ := list.([]any)
	for _, item := range items {
		if r, ok := item.(map[string]any); ok && r["method"] == method && r["route"] == route {
			return r
		}
	}
	return nil
}

// TestObservabilityOverview sends a scripted load (successes, unmatched
// paths with secrets in them, and 503s from maintenance mode) and checks
// the overview's numbers per route, instance and minute, without any
// requested path or query.
func TestObservabilityOverview(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	a.StartWorkers(t) // the collector writes the requests (see obsWaitOverview)
	h := a.Handler()
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	user, _ := a.SignIn(t, "user@example.com", "")

	for range 40 {
		if r := do(t, h, "GET", "/v1/ping", ""); r.Code != http.StatusOK {
			t.Fatalf("GET /v1/ping = %d", r.Code)
		}
	}
	for i := range 12 {
		do(t, h, "GET", fmt.Sprintf("/v1/secret-path-%d?token=hunter2", i), "")
	}
	if r := do(t, h, "PUT", "/ops/settings/maintenance.enabled", `{"value":true,"version":0,"reason":"load test"}`, admin...); r.Code != http.StatusOK {
		t.Fatalf("maintenance on = %d %s", r.Code, r.Body)
	}
	for range 10 {
		if r := do(t, h, "GET", "/v1/ping", ""); r.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET /v1/ping in maintenance = %d", r.Code)
		}
	}
	if r := do(t, h, "DELETE", "/ops/settings/maintenance.enabled", `{"version":1,"reason":"done"}`, admin...); r.Code != http.StatusOK {
		t.Fatalf("maintenance off = %d %s", r.Code, r.Body)
	}

	for _, headers := range [][]string{nil, user} {
		if r := do(t, h, "GET", "/ops/observability/overview", "", headers...); r.Code != http.StatusUnauthorized && r.Code != http.StatusForbidden {
			t.Errorf("GET /ops/observability/overview without the permission = %d", r.Code)
		}
	}
	// Instead of flushRequests: wait until the whole load is written. The
	// overview requests polled meanwhile are counted too, as successes of
	// another route, so the totals below are checked against each other.
	r := obsWaitOverview(t, h, "?window=5m", viewer, func(r response) bool {
		top, _ := r.JSON["top_routes"].(map[string]any)
		ping := obsFindRoute(top["by_requests"], "GET", "/v1/ping")
		unmatched := obsFindRoute(top["by_requests"], "GET", "/")
		errs := obsFindRoute(top["by_errors"], "GET", "")
		return ping != nil && ping["requests"] == float64(40) && unmatched != nil && unmatched["requests"] == float64(12) &&
			errs != nil && errs["server_errors"] == float64(10)
	})
	for _, secret := range []string{"secret-path", "hunter2", "token="} {
		if strings.Contains(r.Body, secret) {
			t.Errorf("the overview holds the requested path or query %q: %s", secret, r.Body)
		}
	}
	total := r.JSON["requests"].(float64)
	if r.JSON["window"] != "5m0s" || r.JSON["server_errors"] != float64(10) || r.JSON["error_rate"] != float64(int64(10/total*10000+0.5))/10000 ||
		r.JSON["requests_per_minute"] != float64(int64(total/5*10+0.5))/10 {
		t.Errorf("totals = requests %v, server errors %v, error rate %v, per minute %v", total, r.JSON["server_errors"], r.JSON["error_rate"], r.JSON["requests_per_minute"])
	}
	latency := r.JSON["latency_ms"].(map[string]any)
	if p50, p95, p99, maxMS := latency["p50"].(float64), latency["p95"].(float64), latency["p99"].(float64), latency["max"].(float64); p50 <= 0 || p95 < p50 || p99 < p95 || maxMS < p99 {
		t.Errorf("latency = %v, want 0 < p50 ≤ p95 ≤ p99 ≤ max", latency)
	}

	top := r.JSON["top_routes"].(map[string]any)
	if ping := obsFindRoute(top["by_requests"], "GET", "/v1/ping"); ping == nil || ping["requests"] != float64(40) || ping["server_errors"] != float64(0) {
		t.Errorf("GET /v1/ping = %v, want 40 requests without errors (the 503s had no route)", ping)
	}
	if unmatched := obsFindRoute(top["by_requests"], "GET", "/"); unmatched == nil || unmatched["requests"] != float64(12) || unmatched["client_errors"] != float64(12) {
		t.Errorf("the catch-all route = %v, want 12 requests, all 404", unmatched)
	}
	byErrors, _ := top["by_errors"].([]any)
	if len(byErrors) != 1 || obsFindRoute(byErrors, "GET", "") == nil || obsFindRoute(byErrors, "GET", "")["server_errors"] != float64(10) {
		t.Errorf("by_errors = %v, want the 10 maintenance responses, answered before routing", byErrors)
	}

	system := do(t, h, "GET", "/ops/system", "", viewer...)
	instances, _ := r.JSON["instances"].([]any)
	if len(instances) != 1 || instances[0].(map[string]any)["instance_id"] != system.JSON["instance"].(map[string]any)["id"] ||
		instances[0].(map[string]any)["requests"] != total {
		t.Errorf("instances = %v, want this instance with every request", instances)
	}
	var perMinute float64
	for _, m := range r.JSON["minutes"].([]any) {
		perMinute += m.(map[string]any)["requests"].(float64)
	}
	if perMinute != total {
		t.Errorf("minutes add up to %v requests, want %v", perMinute, total)
	}

	routes := do(t, h, "GET", "/ops/observability/routes?window=1h&sort=errors&limit=1", "", viewer...)
	if list, _ := routes.JSON["routes"].([]any); routes.Code != http.StatusOK || len(list) != 1 || obsFindRoute(list, "GET", "") == nil {
		t.Errorf("GET /ops/observability/routes?sort=errors&limit=1 = %d %s", routes.Code, routes.Body)
	}
	for _, window := range []string{"90s", "25h", "0m", "soon"} {
		if bad := do(t, h, "GET", "/ops/observability/overview?window="+window, "", viewer...); bad.Code != http.StatusUnprocessableEntity || bad.JSON["code"] != "invalid_observability_window" {
			t.Errorf("window=%s: %d %s, want 422 invalid_observability_window", window, bad.Code, bad.Body)
		}
	}
}

// TestObservabilityAcrossInstances runs two instances on one database: the
// overview on either counts both.
func TestObservabilityAcrossInstances(t *testing.T) {
	first := newTestApp(t, testAppOptions{})
	env := map[string]string{
		"APP_ENV": "development", "APP_ADDR": "127.0.0.1:0", "DATABASE_URL": first.URL, "APP_DB_MAX_CONNS": "8", "APP_JOB_WORKERS": "4",
		"LOG_ARCHIVE_DIR": t.TempDir(), "STORAGE_LOCAL_DIR": t.TempDir(),
	}
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(key string) string { return env[key] }, ReadFile: os.ReadFile})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	second, err := buildTestApp(t, cfg, testAppOptions{})
	if err != nil {
		t.Fatalf("New() second instance error = %v", err)
	}
	// Both instances run, so their collectors write (see obsWaitOverview).
	first.StartWorkers(t)
	second.StartWorkers(t)
	// Harness tokens are per instance, so the viewer signs in to the second
	// instance, where the overview is read.
	viewer, _ := second.SignIn(t, "viewer@example.com", "ops_viewer")
	for range 7 {
		do(t, first.Handler(), "GET", "/v1/ping", "")
	}
	for range 5 {
		do(t, second.Handler(), "GET", "/v1/ping", "")
	}

	r := obsWaitOverview(t, second.Handler(), "", viewer, func(r response) bool {
		instances, _ := r.JSON["instances"].([]any)
		top, _ := r.JSON["top_routes"].(map[string]any)
		ping := obsFindRoute(top["by_requests"], "GET", "/v1/ping")
		return len(instances) == 2 && ping != nil && ping["requests"] == float64(12)
	})
	instances, _ := r.JSON["instances"].([]any)
	ping := obsFindRoute(r.JSON["top_routes"].(map[string]any)["by_requests"], "GET", "/v1/ping")
	if r.Code != http.StatusOK || len(instances) != 2 || ping == nil || ping["requests"] != float64(12) {
		t.Fatalf("overview on the second instance = %d, %d instances, ping %v; want both instances and 12 pings", r.Code, len(instances), ping)
	}
	var sum float64
	for _, in := range instances {
		sum += in.(map[string]any)["requests"].(float64)
	}
	if sum != r.JSON["requests"] {
		t.Errorf("instances add up to %v requests, total %v", sum, r.JSON["requests"])
	}
}

// obsEvent is one Server-Sent Event.
type obsEvent struct{ name, data string }

// obsReadEvents reads a stream's events into a channel until it ends.
func obsReadEvents(body io.Reader) <-chan obsEvent {
	events := make(chan obsEvent, 16)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var e obsEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, "retry: "):
				events <- obsEvent{name: "retry", data: strings.TrimPrefix(line, "retry: ")}
			case line == "" && e.name != "":
				events <- e
				e = obsEvent{}
			}
		}
	}()
	return events
}

func obsNextEvent(t *testing.T, events <-chan obsEvent, within time.Duration) obsEvent {
	t.Helper()
	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("the stream ended without an event")
		}
		return e
	case <-time.After(within):
		t.Fatalf("no event within %v", within)
	}
	return obsEvent{}
}

// TestObservabilityStream checks the live stream over a real server: the
// permission on connect, the per-user limit, events flushed as they come,
// and the end of the stream when the session signs out.
func TestObservabilityStream(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	user, _ := a.SignIn(t, "user@example.com", "")

	open := func(query string, headers []string) *http.Response {
		t.Helper()
		req, err := http.NewRequest("GET", srv.URL+"/ops/observability/stream"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	problem := func(resp *http.Response) string {
		defer resp.Body.Close()
		var p struct{ Code string }
		_ = json.NewDecoder(resp.Body).Decode(&p)
		return fmt.Sprint(resp.StatusCode, " ", p.Code)
	}
	if got := problem(open("", nil)); got != "401 unauthenticated" {
		t.Errorf("stream without a session = %s, want 401 unauthenticated", got)
	}
	if got := problem(open("", user)); got != "403 forbidden" {
		t.Errorf("stream without the permission = %s, want 403 forbidden", got)
	}
	if got := problem(open("?window=30s", viewer)); got != "422 invalid_observability_window" {
		t.Errorf("stream with a 30s window = %s, want 422", got)
	}

	resp := open("?window=1h", viewer)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("X-Accel-Buffering") != "no" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stream = %d %v", resp.StatusCode, resp.Header)
	}
	events := obsReadEvents(resp.Body)
	if e := obsNextEvent(t, events, 5*time.Second); e.name != "retry" || e.data != "5000" {
		t.Errorf("first line = %+v, want retry: 5000", e)
	}
	e := obsNextEvent(t, events, 5*time.Second)
	var overview map[string]any
	if err := json.Unmarshal([]byte(e.data), &overview); e.name != "overview" || err != nil || overview["window"] != "1h0m0s" {
		t.Fatalf("first event = %+v (%v), want an overview of 1h", e, err)
	}

	// A user may hold two streams on an instance.
	second := open("", viewer)
	defer second.Body.Close()
	if got := problem(open("", viewer)); got != "429 observability_streams_limited" {
		t.Errorf("third stream = %s, want 429 observability_streams_limited", got)
	}

	// Signing out ends the stream at its next check. Sign-in isn't a module
	// yet (Phase 5), so the harness revokes the token instead of POST
	// /v1/auth/logout; the stream's check authenticates its request again.
	a.SignOut(viewer)
	for {
		e := obsNextEvent(t, events, 12*time.Second)
		if e.name == "overview" {
			continue // sent before the check noticed
		}
		if e.name != "end" || e.data != `{"reason":"unauthorized"}` {
			t.Errorf("after signing out: %+v, want end with reason unauthorized", e)
		}
		break
	}
	if _, ok := <-events; ok {
		t.Error("the stream stayed open after its end event")
	}
}

// TestIncidentsThroughOps opens, updates and resolves an incident through
// /ops, checks permissions, errors and audit events, and reads its report
// as JSON and Markdown.
func TestIncidentsThroughOps(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	a.StartWorkers(t) // the release tracker records this instance
	h := a.Handler()
	admin, adminID := a.SignIn(t, "admin@example.com", "platform_admin")
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	const open = `{"title":"Checkout <b>failing</b> | now","severity":"sev1","summary":"Payments time out."}`

	if r := do(t, h, "POST", "/ops/incidents", open, viewer...); r.Code != http.StatusForbidden {
		t.Errorf("POST /ops/incidents as ops_viewer = %d, want 403", r.Code)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if r := do(t, h, "POST", "/ops/incidents", `{"title":"Later","severity":"sev2","started_at":"`+future+`"}`, admin...); r.Code != http.StatusUnprocessableEntity || r.JSON["code"] != "invalid_incident" {
		t.Errorf("POST /ops/incidents starting in the future = %d %s, want 422 invalid_incident", r.Code, r.Body)
	}
	created := do(t, h, "POST", "/ops/incidents", open, admin...)
	if created.Code != http.StatusCreated || created.JSON["status"] != "investigating" || created.JSON["source"] != "manual" ||
		created.JSON["created_by"].(map[string]any)["id"] != adminID || len(created.JSON["updates"].([]any)) != 1 {
		t.Fatalf("POST /ops/incidents = %d %s", created.Code, created.Body)
	}
	id := int64(created.JSON["id"].(float64))
	path := fmt.Sprintf("/ops/incidents/%d", id)

	if r := do(t, h, "POST", path+"/updates", `{"message":"Provider outage confirmed.","status":"identified","severity":"sev2"}`, admin...); r.Code != http.StatusCreated ||
		r.JSON["status"] != "identified" || r.JSON["severity"] != "sev2" || len(r.JSON["updates"].([]any)) != 2 {
		t.Errorf("POST %s/updates = %d %s", path, r.Code, r.Body)
	}
	for query, want := range map[string]int{"?status=open": 1, "?status=resolved": 0, "?severity=sev2": 1, "?source=automatic": 0, "": 1} {
		if r := do(t, h, "GET", "/ops/incidents"+query, "", viewer...); r.Code != http.StatusOK || len(r.JSON["incidents"].([]any)) != want {
			t.Errorf("GET /ops/incidents%s = %d %s, want %d incidents", query, r.Code, r.Body, want)
		}
	}
	if r := do(t, h, "POST", path+"/resolve", `{"message":"Provider recovered."}`, admin...); r.Code != http.StatusOK || r.JSON["status"] != "resolved" || r.JSON["resolved_at"] == nil {
		t.Errorf("POST %s/resolve = %d %s", path, r.Code, r.Body)
	}
	if r := do(t, h, "POST", path+"/updates", `{"message":"late"}`, admin...); r.Code != http.StatusConflict || r.JSON["code"] != "incident_resolved" {
		t.Errorf("updating a resolved incident = %d %s, want 409 incident_resolved", r.Code, r.Body)
	}
	if r := do(t, h, "GET", "/ops/incidents/999999", "", viewer...); r.Code != http.StatusNotFound || r.JSON["code"] != "incident_not_found" {
		t.Errorf("GET /ops/incidents/999999 = %d %s", r.Code, r.Body)
	}
	if r := do(t, h, "GET", path, "", viewer...); r.Code != http.StatusOK || len(r.JSON["updates"].([]any)) != 3 {
		t.Errorf("GET %s = %d %s, want 3 updates", path, r.Code, r.Body)
	}

	audit := do(t, h, "GET", "/ops/audit?action_prefix=ops.incident.", "", viewer...)
	events, _ := audit.JSON["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("incident audit events = %s, want 3", audit.Body)
	}
	for i, action := range []string{"ops.incident.resolved", "ops.incident.updated", "ops.incident.opened"} {
		e := events[i].(map[string]any)
		if e["action"] != action || e["resource_id"] != fmt.Sprint(id) || e["actor_id"] != adminID || strings.Contains(fmt.Sprint(e), "Checkout") {
			t.Errorf("audit event %d = %v, want %s by the admin without the title", i, e, action)
		}
	}

	// Instead of flushRequests: wait until the collector wrote the requests
	// the report counts (see obsWaitOverview).
	var report response
	deadline := time.Now().Add(60 * time.Second)
	for {
		report = do(t, h, "GET", path+"/report", "", viewer...)
		if requests, _ := report.JSON["requests"].(map[string]any); requests != nil && requests["requests"].(float64) >= 5 || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if report.Code != http.StatusOK || !strings.HasPrefix(report.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("GET %s/report = %d %v %s", path, report.Code, report.Header, report.Body)
	}
	requests, _ := report.JSON["requests"].(map[string]any)
	releases, _ := report.JSON["releases"].([]any)
	auditEvents, _ := report.JSON["audit_events"].([]any)
	if requests == nil || requests["requests"].(float64) < 5 || len(releases) != 1 || len(auditEvents) < 3 ||
		len(report.JSON["incident"].(map[string]any)["updates"].([]any)) != 3 {
		t.Errorf("report = requests %v, releases %v, %d audit events", requests, releases, len(auditEvents))
	}
	// Audit events carry what identifies them, never the client's address
	// or user agent that audit stores.
	for _, leak := range []string{"192.0.2.1", `"ip"`, "user_agent", "metadata", "actor_label"} {
		if strings.Contains(report.Body, leak) {
			t.Errorf("the report holds %q", leak)
		}
	}

	for _, headers := range [][]string{append([]string{"Accept", "text/markdown"}, viewer...), viewer} {
		target := path + "/report"
		if len(headers) == len(viewer) {
			target += "?format=markdown"
		}
		md := do(t, h, "GET", target, "", headers...)
		if md.Code != http.StatusOK || md.Header.Get("Content-Type") != "text/markdown; charset=utf-8" {
			t.Fatalf("GET %s = %d %v", target, md.Code, md.Header)
		}
		for _, want := range []string{
			fmt.Sprintf("# Incident %d: Checkout &lt;b&gt;failing&lt;/b&gt; \\| now", id),
			"| Severity | sev2 |", "## Timeline", "Provider outage confirmed.", "## Requests", "## Releases", "ops.incident.opened",
		} {
			if !strings.Contains(md.Body, want) {
				t.Errorf("Markdown report lacks %q:\n%s", want, md.Body)
			}
		}
		if strings.Contains(md.Body, "<b>") {
			t.Error("Markdown report holds unescaped HTML from the title")
		}
	}
}

// TestIncidentDetectionThroughJob writes a minute of failing requests and
// runs incidents_detect: an automatic incident opens, recorded by the job.
func TestIncidentDetectionThroughJob(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	a.StartWorkers(t)
	h := a.Handler()
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")

	// The golden test opened its own pool on the database URL; the app's
	// pool is the same database.
	store, err := observability.NewStore(a.Deps().DB)
	if err != nil {
		t.Fatal(err)
	}
	minute := observability.Minute{
		Start: time.Now().Truncate(time.Minute), Instance: "other-instance", Method: "GET", Route: "/v1/ping",
		Stats: observability.Stats{Requests: 400, ServerErrors: 100},
	}
	if err := store.WriteMinutes(context.Background(), []observability.Minute{minute}); err != nil {
		t.Fatal(err)
	}

	def := do(t, h, "GET", "/ops/jobs/definitions/incidents_detect", "", admin...)
	if config, _ := def.JSON["config"].(map[string]any); def.Code != http.StatusOK || config["schedule"] != "@every 1m" || config["enabled"] != true {
		t.Errorf("incidents_detect definition = %d %s", def.Code, def.Body)
	}
	if r := do(t, h, "POST", "/ops/jobs/definitions/incidents_detect/run", "", admin...); r.Code >= 300 {
		t.Fatalf("run incidents_detect = %d %s", r.Code, r.Body)
	}
	var incidents []any
	waitFor(t, "an automatic incident", func() bool {
		incidents, _ = do(t, h, "GET", "/ops/incidents?source=automatic", "", admin...).JSON["incidents"].([]any)
		return len(incidents) == 1
	})
	inc := incidents[0].(map[string]any)
	if inc["severity"] != "sev2" || inc["status"] != "investigating" || inc["created_by"].(map[string]any)["id"] != "incidents_detect" {
		t.Errorf("automatic incident = %v", inc)
	}
	events, _ := do(t, h, "GET", "/ops/audit?action=ops.incident.opened", "", admin...).JSON["events"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["actor_id"] != "incidents_detect" || events[0].(map[string]any)["actor_kind"] != "system" {
		t.Errorf("audit events = %v, want ops.incident.opened by system incidents_detect", events)
	}
}
