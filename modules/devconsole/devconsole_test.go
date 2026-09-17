package devconsole_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/httpx"
	gmail "gorbital.dev/mail"
	"gorbital.dev/modules/devconsole"
)

const token = "0123456789abcdef0123456789abcdef0123456789a"

// newServer serves app behind a console with opts on a real loopback
// listener and returns its base URL (http://127.0.0.1:port) and console.
func newServer(t *testing.T, app http.Handler, opts ...devconsole.Option) (string, *devconsole.Console) {
	t.Helper()
	c, err := devconsole.New(token, opts...)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(c.Mount(app, slog.New(slog.DiscardHandler)))
	t.Cleanup(func() {
		c.Close()
		srv.Close()
	})
	return srv.URL, c
}

type result struct {
	code   int
	header http.Header
	body   string
}

// get requests path from base with the Host header host ("" keeps the
// URL's) and the token when withToken.
func get(t *testing.T, base, path, host string, withToken bool, headers ...string) result {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if withToken {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return result{resp.StatusCode, resp.Header, string(body)}
}

func problemCode(t *testing.T, body string) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("not a problem: %q", body)
	}
	return p.Code
}

func appHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "app") })
}

func TestCheckToken(t *testing.T) {
	for tok, ok := range map[string]bool{
		token:                         true,
		strings.Repeat("x", 32):       true,
		strings.Repeat("x", 31):       false,
		strings.Repeat("x", 513):      false,
		"":                            false,
		strings.Repeat("x", 31) + " ": false,
		strings.Repeat("é", 20):       false,
	} {
		if err := devconsole.CheckToken(tok); (err == nil) != ok {
			t.Errorf("CheckToken(%q) = %v, want ok %v", tok, err, ok)
		}
		if _, err := devconsole.New(tok); (err == nil) != ok || (!ok && !errors.Is(err, devconsole.ErrInvalidToken)) {
			t.Errorf("New(%q) error = %v", tok, err)
		}
	}
}

func TestConsoleRefusesOtherHosts(t *testing.T) {
	base, _ := newServer(t, appHandler())
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(base, "http://"))

	for _, host := range []string{"localhost:" + port, "127.0.0.1:" + port, "[::1]:" + port} {
		if r := get(t, base, "/_dev/", host, true); r.code != http.StatusOK {
			t.Errorf("Host %s = %d %s, want 200", host, r.code, r.body)
		}
	}
	for _, host := range []string{
		"evil.example", "evil.example:" + port, "localhost.evil.example:" + port, "127.0.0.1.nip.io:" + port,
		"localhost", "localhost:1", "[::ffff:127.0.0.1]:" + port, "[0:0:0:0:0:0:0:1]:" + port, "::1", "0.0.0.0:" + port,
	} {
		r := get(t, base, "/_dev/app", host, true)
		if r.code != http.StatusForbidden || problemCode(t, r.body) != "forbidden" {
			t.Errorf("Host %q = %d %s, want 403 forbidden", host, r.code, r.body)
		}
		if strings.Contains(r.body, "endpoints") {
			t.Errorf("Host %q response leaks console content: %s", host, r.body)
		}
	}
	// Other paths still reach the app whatever the host.
	if r := get(t, base, "/v1/ping", "evil.example", false); r.code != http.StatusOK || r.body != "app" {
		t.Errorf("app path = %d %q", r.code, r.body)
	}
}

func TestConsoleRefusesMissingHost(t *testing.T) {
	base, _ := newServer(t, appHandler())
	conn, err := net.Dial("tcp", strings.TrimPrefix(base, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// HTTP/1.0 allows a request without Host; net/http serves it with an
	// empty r.Host.
	fmt.Fprintf(conn, "GET /_dev/ HTTP/1.0\r\nAuthorization: Bearer %s\r\n\r\n", token)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("request without Host = %d, want 403", resp.StatusCode)
	}
}

func TestConsoleRequiresToken(t *testing.T) {
	base, _ := newServer(t, appHandler())
	for name, header := range map[string]string{
		"missing":          "",
		"wrong":            "Bearer " + strings.Repeat("z", len(token)),
		"prefix":           "Bearer " + token[:len(token)-1],
		"longer":           "Bearer " + token + "0",
		"no scheme":        token,
		"basic":            "Basic " + token,
		"in another value": "Bearer x, Bearer " + token,
	} {
		r := get(t, base, "/_dev/app", "", false, "Authorization", header)
		if r.code != http.StatusUnauthorized || problemCode(t, r.body) != "unauthorized" {
			t.Errorf("%s token = %d %s, want 401 unauthorized", name, r.code, r.body)
		}
		if !strings.HasPrefix(r.header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s token WWW-Authenticate = %q", name, r.header.Get("WWW-Authenticate"))
		}
	}
	// The token isn't accepted in the query string either.
	if r := get(t, base, "/_dev/?token="+token, "", false); r.code != http.StatusUnauthorized {
		t.Errorf("token in the query = %d, want 401", r.code)
	}
}

func TestConsoleRefusesRemotePeers(t *testing.T) {
	c, err := devconsole.New(token, devconsole.WithAddr("127.0.0.1:8080"))
	if err != nil {
		t.Fatal(err)
	}
	h := c.Mount(appHandler(), nil)
	for peer, want := range map[string]int{"127.0.0.1:5000": 200, "[::1]:5000": 200, "192.168.1.10:5000": 403, "10.0.0.2:5000": 403} {
		req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/_dev/", nil)
		req.RemoteAddr = peer
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("peer %s = %d, want %d", peer, rec.Code, want)
		}
	}
}

func TestConsoleHeaders(t *testing.T) {
	base, _ := newServer(t, appHandler())
	checks := []struct {
		name      string
		method    string
		path      string
		withToken bool
		headers   []string
	}{
		{"success", http.MethodGet, "/_dev/", true, []string{"Origin", "https://evil.example"}},
		{"unauthorized", http.MethodGet, "/_dev/app", false, []string{"Origin", "http://localhost:3000"}},
		{"preflight", http.MethodOptions, "/_dev/requests", false, []string{"Origin", "http://localhost:3000", "Access-Control-Request-Method", "GET", "Access-Control-Request-Headers", "authorization"}},
		{"not found", http.MethodGet, "/_dev/nope", true, nil},
		{"method", http.MethodPost, "/_dev/requests", true, nil},
	}
	for _, c := range checks {
		req, _ := http.NewRequest(c.method, base+c.path, nil)
		if c.withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(c.headers); i += 2 {
			req.Header.Set(c.headers[i], c.headers[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		for name := range resp.Header {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				t.Errorf("%s: response has CORS header %s", c.name, name)
			}
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", c.name, got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", c.name, got)
		}
	}
}

func TestConsoleEndpointsWithoutSources(t *testing.T) {
	base, _ := newServer(t, appHandler())
	r := get(t, base, "/_dev", "", true)
	var index devconsole.Index
	if err := json.Unmarshal([]byte(r.body), &index); err != nil || r.code != 200 {
		t.Fatalf("index = %d %s", r.code, r.body)
	}
	want := []string{"/_dev/", "/_dev/openapi.json", "/_dev/requests", "/_dev/requests/stream"}
	if strings.Join(index.Endpoints, ",") != strings.Join(want, ",") {
		t.Errorf("endpoints = %v, want %v", index.Endpoints, want)
	}
	for _, path := range []string{"/_dev/app", "/_dev/logs", "/_dev/mail", "/_dev/jobs", "/_dev/migrations"} {
		if r := get(t, base, path, "", true); r.code != http.StatusNotFound {
			t.Errorf("%s without a source = %d, want 404", path, r.code)
		}
	}
	if r := get(t, base, "/_dev/../_dev/app", "", true); r.code != http.StatusNotFound {
		t.Errorf("dotted path = %d", r.code)
	}
}

func TestNilConsole(t *testing.T) {
	var c *devconsole.Console
	h := c.Mount(appHandler(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_dev/app", nil))
	if rec.Body.String() != "app" {
		t.Errorf("nil console served %q, want the app", rec.Body.String())
	}
	c.RecordRequest(devconsole.Request{})
	c.Close()
	if mw := c.Middleware()(appHandler()); mw == nil {
		t.Error("nil console middleware is nil")
	}
	var logs *devconsole.Logs
	if logs.Handler() != nil {
		t.Error("nil logs handler isn't nil")
	}
}

func TestRequestsNeverHoldQueriesOrHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	var c *devconsole.Console
	app := http.Handler(nil)
	base, console := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { app.ServeHTTP(w, r) }), devconsole.WithMaxRequests(3))
	c = console
	// A middleware copying the request between, as authentication does.
	copyRequest := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r.WithContext(r.Context())) })
	}
	app = httpx.Chain(devconsole.RecordRoute(mux), c.Middleware(), copyRequest)

	secrets := []string{"querysecret", "headersecret", "cookiesecret", "bodysecret"}
	for i := range 5 {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/items/%d?token=querysecret&x=1", base, i), strings.NewReader("bodysecret"))
		req.Header.Set("Authorization", "Bearer headersecret")
		req.Header.Set("Cookie", "session=cookiesecret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	c.RecordRequest(devconsole.Request{Method: "BREW", Path: "/raw?token=querysecret", Status: 200})

	r := get(t, base, "/_dev/requests", "", true)
	for _, s := range secrets {
		if strings.Contains(r.body, s) {
			t.Errorf("/_dev/requests holds %q: %s", s, r.body)
		}
	}
	var list devconsole.RequestList
	if err := json.Unmarshal([]byte(r.body), &list); err != nil {
		t.Fatal(err)
	}
	if list.Max != 3 || len(list.Requests) != 3 {
		t.Fatalf("requests = %d (max %d), want the 3 newest", len(list.Requests), list.Max)
	}
	newest, item := list.Requests[0], list.Requests[1]
	if newest.Path != "/raw" || newest.Method != "_OTHER" {
		t.Errorf("newest = %+v, want path /raw and method _OTHER", newest)
	}
	if item.Path != "/v1/items/4" || item.Route != "/v1/items/{id}" || item.Status != http.StatusTeapot || item.Method != "GET" {
		t.Errorf("request = %+v", item)
	}
}

func TestLogsAreBounded(t *testing.T) {
	logs, err := devconsole.NewLogs(5)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(logs.Handler()).With("component", "test").WithGroup("req")
	logger.Debug("debug is left out")
	for i := range 20 {
		logger.Info(fmt.Sprintf("line %d", i), "i", i)
	}
	many := make([]any, 0, 200)
	for i := range 100 {
		many = append(many, fmt.Sprintf("k%d", i), strings.Repeat("v", 5000))
	}
	logger.Warn(strings.Repeat("m", 10_000), many...)
	logger.Error("secret", "password", config.NewSecret("hunter2-secret"), "err", errors.New("boom"))

	got := logs.List()
	if len(got) != 5 {
		t.Fatalf("records = %d, want 5", len(got))
	}
	long := got[3]
	size := len(long.Message)
	for _, a := range long.Attrs {
		size += len(a.Key) + len(a.Value)
		if len(a.Value) > 1100 {
			t.Errorf("attr %s holds %d bytes", a.Key, len(a.Value))
		}
	}
	// component and 100 attributes of 5,000 bytes: the message (4 KiB) and
	// what fits in 8 KiB are kept, the rest counted.
	if len(long.Message) > 4100 || size > 8<<10 || len(long.Attrs)+long.DroppedAttrs != 101 || long.DroppedAttrs < 90 {
		t.Errorf("long record: message %d bytes, %d bytes in all, %d attrs, %d dropped", len(long.Message), size, len(long.Attrs), long.DroppedAttrs)
	}
	many = many[:0]
	for i := range 80 {
		many = append(many, fmt.Sprintf("k%d", i), i)
	}
	logger.Info("small attributes", many...)
	if last := logs.List()[4]; len(last.Attrs) != 50 || last.DroppedAttrs != 31 {
		t.Errorf("81 small attributes: %d kept, %d dropped; want 50 and 31", len(last.Attrs), last.DroppedAttrs)
	}
	last := got[4]
	data, _ := json.Marshal(last)
	logger.Info("filler") // keeps five records after the one above
	if strings.Contains(string(data), "hunter2") || !strings.Contains(string(data), `"req.password","value":"[redacted]"`) ||
		!strings.Contains(string(data), `"component","value":"test"`) || !strings.Contains(string(data), "boom") || last.Level != "ERROR" {
		t.Errorf("record = %s", data)
	}
	if got[0].Message != "line 17" {
		t.Errorf("oldest kept = %q, want line 17", got[0].Message)
	}
}

func TestEnvKeys(t *testing.T) {
	var e devconsole.EnvKeys
	e.Read("APP_ADDR", "127.0.0.1:8080")
	e.Read("APP_CORS_ORIGINS", "")
	e.ReadSecret("DATABASE_URL", true)
	e.Read("GITHUB_CLIENT_SECRET", "ghsecret-value") // read as plain by mistake
	e.Read("OTEL_EXPORTER_OTLP_ENDPOINT", "https://user:pa55@otel.example:4318/v1?api_key=abc#frag")
	e.Read("APPLE_KEY_ID", "KEYID123")
	e.ReadSecret("DEV_CONSOLE_TOKEN", true)
	e.Read("DEV_CONSOLE_TOKEN", token) // a secret stays a secret

	data, _ := json.Marshal(e.List())
	for _, leak := range []string{"ghsecret-value", "pa55", "user:", "abc", "frag", "KEYID123", token} {
		if strings.Contains(string(data), leak) {
			t.Errorf("env keys hold %q: %s", leak, data)
		}
	}
	want := `[{"name":"APPLE_KEY_ID","secret":true,"set":true},{"name":"APP_ADDR","secret":false,"set":true,"value":"127.0.0.1:8080"},` +
		`{"name":"APP_CORS_ORIGINS","secret":false,"set":false},{"name":"DATABASE_URL","secret":true,"set":true},` +
		`{"name":"DEV_CONSOLE_TOKEN","secret":true,"set":true},{"name":"GITHUB_CLIENT_SECRET","secret":true,"set":true},` +
		`{"name":"OTEL_EXPORTER_OTLP_ENDPOINT","secret":false,"set":true,"value":"https://[redacted]@otel.example:4318/v1?[redacted]#[redacted]"}]`
	if string(data) != want {
		t.Errorf("env keys = %s\nwant %s", data, want)
	}
}

func TestRoutesFromOpenAPI(t *testing.T) {
	doc := `{"openapi":"3.1.0","paths":{
		"/v1/items/{id}":{"delete":{"operationId":"delete-item","tags":["Items"],"security":[{"bearer":[]}]},"get":{"operationId":"get-item","summary":"Get","tags":["Items"]}},
		"/livez":{"get":{"operationId":"live"},"parameters":[]}}}`
	routes, err := devconsole.RoutesFromOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(routes)
	want := `[{"method":"GET","path":"/livez","operation_id":"live","tags":[],"secured":false,"source":"openapi"},` +
		`{"method":"DELETE","path":"/v1/items/{id}","operation_id":"delete-item","tags":["Items"],"secured":true,"source":"openapi"},` +
		`{"method":"GET","path":"/v1/items/{id}","operation_id":"get-item","summary":"Get","tags":["Items"],"secured":false,"source":"openapi"}]`
	if string(got) != want {
		t.Errorf("routes = %s\nwant %s", got, want)
	}
	if _, err := devconsole.RoutesFromOpenAPI([]byte("{")); err == nil {
		t.Error("invalid document accepted")
	}
}

func TestMailpitSource(t *testing.T) {
	mailpit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" || r.URL.Query().Get("limit") != "50" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"total":2,"unread":1,"count":1,"messages":[{"ID":"m1","MessageID":"x@y","Read":false,
			"From":{"Name":"Acme","Address":"no-reply@acme.test"},"To":[{"Name":"","Address":"ada@acme.test"}],"Cc":[],
			"Subject":"Verify your email","Created":"2026-09-16T10:00:00Z","Tags":[],"Size":1234,"Attachments":0,"Snippet":"Your code is 123456"}]}`)
	}))
	defer mailpit.Close()

	source, err := devconsole.MailpitSource(mailpit.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := newServer(t, appHandler(), devconsole.WithSources(devconsole.Sources{Mail: source}))
	r := get(t, base, "/_dev/mail", "", true)
	var mail devconsole.Mail
	if err := json.Unmarshal([]byte(r.body), &mail); err != nil || r.code != 200 {
		t.Fatalf("mail = %d %s", r.code, r.body)
	}
	if mail.Total != 2 || mail.WebURL != mailpit.URL || len(mail.Messages) != 1 || mail.Messages[0].To[0].Address != "ada@acme.test" ||
		mail.Messages[0].From.Name != "Acme" || mail.Messages[0].Size != 1234 {
		t.Errorf("mail = %+v", mail)
	}

	mailpit.Close()
	if r := get(t, base, "/_dev/mail", "", true); r.code != http.StatusServiceUnavailable || problemCode(t, r.body) != "unavailable" {
		t.Errorf("Mailpit down = %d %s, want 503 unavailable", r.code, r.body)
	}
	for _, bad := range []string{"ftp://host", "http://", "http://u:p@host:8025", "::"} {
		if _, err := devconsole.MailpitSource(bad, nil); err == nil {
			t.Errorf("MailpitSource(%q) accepted", bad)
		}
	}
}

func TestSourceErrors(t *testing.T) {
	base, _ := newServer(t, appHandler(), devconsole.WithSources(devconsole.Sources{
		Jobs: func(context.Context) ([]devconsole.JobRun, error) {
			return nil, errors.New("database password=hunter2 failed")
		},
	}))
	r := get(t, base, "/_dev/jobs", "", true)
	if r.code != http.StatusInternalServerError || strings.Contains(r.body, "hunter2") {
		t.Errorf("failing source = %d %s, want 500 without the error", r.code, r.body)
	}
}

// event is one Server-Sent Event.
type event struct{ name, data string }

func readEvents(body io.Reader) <-chan event {
	events := make(chan event, 100)
	go func() {
		defer close(events)
		sc := bufio.NewScanner(body)
		var e event
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			case line == "" && e.name != "":
				events <- e
				e = event{}
			}
		}
	}()
	return events
}

func openStream(t *testing.T, base, path string) (*http.Response, <-chan event) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, readEvents(resp.Body)
}

func next(t *testing.T, events <-chan event) event {
	t.Helper()
	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("stream ended")
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	return event{}
}

func TestStreams(t *testing.T) {
	logs, _ := devconsole.NewLogs(10)
	base, c := newServer(t, appHandler(), devconsole.WithLogs(logs), devconsole.WithStreams(2, 300*time.Millisecond))

	resp, requests := openStream(t, base, "/_dev/requests/stream")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stream = %d %v", resp.StatusCode, resp.Header)
	}
	_, logEvents := openStream(t, base, "/_dev/logs/stream")
	time.Sleep(50 * time.Millisecond) // both subscribed

	// A third stream is refused while two run.
	if r := get(t, base, "/_dev/logs/stream", "", true); r.code != http.StatusTooManyRequests || problemCode(t, r.body) != "rate_limited" {
		t.Errorf("third stream = %d %s, want 429", r.code, r.body)
	}

	c.RecordRequest(devconsole.Request{Method: "GET", Path: "/v1/ping", Route: "/v1/ping", Status: 200})
	if e := next(t, requests); e.name != "request" || !strings.Contains(e.data, `"/v1/ping"`) {
		t.Errorf("event = %+v", e)
	}
	slog.New(logs.Handler()).Info("hello stream")
	if e := next(t, logEvents); e.name != "log" || !strings.Contains(e.data, "hello stream") {
		t.Errorf("event = %+v", e)
	}
	// Streams end after their maximum duration.
	if e := next(t, requests); e.name != "end" || e.data != `{"reason":"max_duration"}` {
		t.Errorf("event = %+v, want end max_duration", e)
	}
	_ = next(t, logEvents)

	// Close ends streams and refuses new ones.
	base2, c2 := newServer(t, appHandler(), devconsole.WithStreams(2, time.Minute))
	_, events := openStream(t, base2, "/_dev/requests/stream")
	time.Sleep(50 * time.Millisecond)
	c2.Close()
	if e := next(t, events); e.name != "end" || e.data != `{"reason":"shutdown"}` {
		t.Errorf("event = %+v, want end shutdown", e)
	}
	if r := get(t, base2, "/_dev/requests/stream", "", true); r.code != http.StatusServiceUnavailable {
		t.Errorf("stream after Close = %d, want 503", r.code)
	}
}

func TestStreamReportsDroppedEvents(t *testing.T) {
	base, c := newServer(t, appHandler())
	req, _ := http.NewRequest(http.MethodGet, base+"/_dev/requests/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	time.Sleep(50 * time.Millisecond)
	// Nobody reads while far more requests than the buffers hold arrive.
	for range 5000 {
		c.RecordRequest(devconsole.Request{Method: "GET", Path: "/" + strings.Repeat("p", 400), Status: 200})
	}
	events := readEvents(resp.Body)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("stream ended without a dropped event")
			}
			if e.name == "dropped" {
				return
			}
			if e.name == "request" {
				c.RecordRequest(devconsole.Request{Method: "GET", Path: "/next", Status: 200})
			}
		case <-deadline:
			t.Fatal("no dropped event")
		}
	}
}

func TestMailPreviews(t *testing.T) {
	var sent []gmail.Message
	previews := &devconsole.MailPreviewer{
		Previews: []devconsole.MailPreview{{Name: "auth.code", Description: "d", Category: "auth"}},
		Build: func(_ context.Context, name, to string) (gmail.Message, error) {
			return gmail.Message{To: []gmail.Address{{Email: to}}, Subject: "Code for " + to, Text: "483920", HTML: "<b>483920</b>", Tags: map[string]string{"category": "auth"}}, nil
		},
		Send: func(_ context.Context, m gmail.Message) error { sent = append(sent, m); return nil },
	}
	base, _ := newServer(t, appHandler(), devconsole.WithSources(devconsole.Sources{MailPreviews: previews}))
	if r := get(t, base, "/_dev/mail/previews", "", true); r.code != 200 || !strings.Contains(r.body, `"name":"auth.code"`) {
		t.Errorf("previews = %d %s", r.code, r.body)
	}
	if r := get(t, base, "/_dev/mail/preview?name=auth.code", "", true); r.code != 200 || !strings.Contains(r.body, `"subject":"Code for preview@example.com"`) || !strings.Contains(r.body, `483920`) || !strings.Contains(r.body, `"to":"preview@example.com"`) {
		t.Errorf("preview = %d %s", r.code, r.body)
	}
	if r := get(t, base, "/_dev/mail/preview?name=nope", "", true); r.code != 404 || !strings.Contains(r.body, "preview_not_found") {
		t.Errorf("unknown preview = %d %s", r.code, r.body)
	}
	if r := get(t, base, "/_dev/mail/preview/send?name=auth.code", "", true); r.code != 405 {
		t.Errorf("GET send = %d %s", r.code, r.body)
	}
	if r := post(t, base, "/_dev/mail/preview/send?name=auth.code&to=ada@example.com", ""); r.code != 200 || !strings.Contains(r.body, `"sent":true`) || len(sent) != 1 || sent[0].To[0].Email != "ada@example.com" {
		t.Errorf("send = %d %s, sent %+v", r.code, r.body, sent)
	}
	if r := post(t, base, "/_dev/mail/preview/send?name=auth.code&to=nope", ""); r.code != 400 || !strings.Contains(r.body, "invalid_address") {
		t.Errorf("bad address = %d %s", r.code, r.body)
	}
	// Without a previewer the endpoints aren't served.
	base2, _ := newServer(t, appHandler())
	if r := get(t, base2, "/_dev/mail/previews", "", true); r.code != 404 {
		t.Errorf("without previews = %d", r.code)
	}
}

// post sends a POST with the token and returns the answer.
func post(t *testing.T, base, path, body string) result {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return result{resp.StatusCode, resp.Header, string(out)}
}

// TestConsoleRefusesTunnelledRequests simulates requests arriving through a
// tunnel on this machine (orb dev --tunnel, ADR-0086): cloudflared connects
// from loopback, forwards the public Host (or, configured to, a local one)
// and adds Cloudflare's forwarding headers. None may reach the console, even
// with the token.
func TestConsoleRefusesTunnelledRequests(t *testing.T) {
	base, _ := newServer(t, appHandler())
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(base, "http://"))
	cloudflare := []string{
		"Cf-Connecting-Ip", "203.0.113.7",
		"X-Forwarded-For", "203.0.113.7",
		"X-Forwarded-Proto", "https",
		"Cf-Ray", "8c0ffee000000000-AMS",
		"Cf-Visitor", `{"scheme":"https"}`,
		"Cdn-Loop", "cloudflare",
	}
	for _, host := range []string{"calm-river-demo.trycloudflare.com", "dev-api.example.com", "localhost:" + port, "127.0.0.1:" + port} {
		for _, path := range []string{"/_dev/", "/_dev/config", "/_dev"} {
			r := get(t, base, path, host, true, cloudflare...)
			if r.code != http.StatusForbidden || problemCode(t, r.body) != "forbidden" || strings.Contains(r.body, "endpoints") {
				t.Errorf("tunnelled %s%s = %d %s, want 403 forbidden", host, path, r.code, r.body)
			}
		}
	}
	// Any one forwarding header is enough, whatever its value.
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "True-Client-Ip", "Cf-Connecting-Ip", "Cf-Ray", "Cdn-Loop"} {
		if r := get(t, base, "/_dev/", "localhost:"+port, true, header, "127.0.0.1"); r.code != http.StatusForbidden || !strings.Contains(r.body, "forwarded by a proxy or tunnel") {
			t.Errorf("local request with %s = %d %s, want 403", header, r.code, r.body)
		}
	}
	// A direct local request still works, and the app's own routes go
	// through the tunnel as before.
	if r := get(t, base, "/_dev/", "localhost:"+port, true); r.code != http.StatusOK {
		t.Errorf("direct local request = %d %s", r.code, r.body)
	}
	if r := get(t, base, "/v1/ping", "calm-river-demo.trycloudflare.com", false, cloudflare...); r.code != http.StatusOK || r.body != "app" {
		t.Errorf("app route through the tunnel = %d %q", r.code, r.body)
	}
}

// TestExtensions: an extension answers under its prefix, for any method,
// only after the console's checks, with the console's headers; the index
// lists it; New refuses prefixes that aren't directories under /_dev/ or
// overlap the console's own endpoints.
func TestExtensions(t *testing.T) {
	var calls int
	ext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, r.Method+" "+r.URL.Path)
	})
	base, _ := newServer(t, appHandler(), devconsole.WithSources(devconsole.Sources{
		Extensions: []devconsole.Extension{{Prefix: "/_dev/auth/test/", Handler: ext}},
	}))

	if r := get(t, base, "/_dev/auth/test/results/x", "", true); r.code != 200 || r.body != "GET /_dev/auth/test/results/x" || r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("GET extension = %d %q %v", r.code, r.body, r.header)
	}
	if r := post(t, base, "/_dev/auth/test/google/start", `{}`); r.code != 200 || r.body != "POST /_dev/auth/test/google/start" {
		t.Errorf("POST extension = %d %q", r.code, r.body)
	}
	calls = 0
	for name, r := range map[string]result{
		"no token":  get(t, base, "/_dev/auth/test/", "", false),
		"tunnelled": get(t, base, "/_dev/auth/test/", "", true, "Cf-Connecting-Ip", "203.0.113.7"),
		"public":    get(t, base, "/_dev/auth/test/", "dev.example.com", true),
	} {
		if r.code != http.StatusUnauthorized && r.code != http.StatusForbidden {
			t.Errorf("%s: extension = %d, want refused", name, r.code)
		}
	}
	if calls != 0 {
		t.Errorf("the extension ran %d times for refused requests", calls)
	}
	if r := get(t, base, "/_dev/auth/other", "", true); r.code != http.StatusNotFound {
		t.Errorf("outside the prefix = %d, want 404", r.code)
	}
	var index devconsole.Index
	_ = json.Unmarshal([]byte(get(t, base, "/_dev/", "", true).body), &index)
	if len(index.Extensions) != 1 || index.Extensions[0] != "/_dev/auth/test/" {
		t.Errorf("index extensions = %v", index.Extensions)
	}

	for _, prefix := range []string{"", "/_dev/", "/_dev/auth", "/auth/test/", "/_dev/mail/", "/_dev/logs/x/", "/_dev//x/"} {
		_, err := devconsole.New(token, devconsole.WithSources(devconsole.Sources{Extensions: []devconsole.Extension{{Prefix: prefix, Handler: ext}}}))
		if err == nil {
			t.Errorf("New with extension prefix %q: no error", prefix)
		}
	}
	twice := []devconsole.Extension{{Prefix: "/_dev/a/", Handler: ext}, {Prefix: "/_dev/a/", Handler: ext}}
	if _, err := devconsole.New(token, devconsole.WithSources(devconsole.Sources{Extensions: twice})); err == nil {
		t.Error("New with the same extension twice: no error")
	}
}
