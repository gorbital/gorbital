package devconsole_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/devconsole"
)

const operatorToken = "operator-test-token-0123456789abcdefghij"

// operatorServer serves next behind Operator over a real loopback listener,
// so the peer and local port are real.
func operatorServer(t *testing.T, console *devconsole.Console) (*httptest.Server, *actor.Actor) {
	t.Helper()
	var seen actor.Actor
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = actor.FromOrAnonymous(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	operator := actor.Actor{Kind: actor.KindSystem, ID: "dev-console", Label: "dev console (orb dev)", Permissions: []string{"ops.settings.read"}}
	srv := httptest.NewServer(console.Operator("/ops/", operator, slog.New(slog.DiscardHandler))(next))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestOperatorGrantsTheTokenOnOpsOnly(t *testing.T) {
	console, err := devconsole.New(operatorToken)
	if err != nil {
		t.Fatal(err)
	}
	srv, seen := operatorServer(t, console)
	get := func(path string, headers ...string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+operatorToken)
		for i := 0; i+1 < len(headers); i += 2 {
			if headers[i] == "Host" {
				req.Host = headers[i+1]
			} else {
				req.Header.Set(headers[i], headers[i+1])
			}
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]

	if code := get("/ops/settings"); code != http.StatusNoContent || seen.Kind != actor.KindSystem || seen.ID != "dev-console" || !seen.Can("ops.settings.read") {
		t.Errorf("/ops/settings with the token = %d, actor %+v; want the operator", code, *seen)
	}
	if get("/ops/settings", "Host", "localhost:"+port); seen.ID != "dev-console" {
		t.Errorf("Host localhost: actor %+v", *seen)
	}
	if get("/v1/ping"); seen.Kind != actor.KindAnonymous {
		t.Errorf("/v1/ping with the token: actor %+v, want anonymous", *seen)
	}
	if get("/ops/settings", "Authorization", "Bearer wrong-"+operatorToken); seen.Kind != actor.KindAnonymous {
		t.Errorf("wrong token: actor %+v, want anonymous", *seen)
	}
	if get("/ops/settings", "Authorization", ""); seen.Kind != actor.KindAnonymous {
		t.Errorf("no token: actor %+v, want anonymous", *seen)
	}
	for _, host := range []string{"evil.example:" + port, "localhost:1", "127.0.0.1.nip.io:" + port} {
		if code := get("/ops/settings", "Host", host); code != http.StatusNoContent || seen.Kind != actor.KindAnonymous {
			t.Errorf("Host %s: %d, actor %+v; want the request through without the operator", host, code, *seen)
		}
	}
	// A cookie never carries the token.
	if get("/ops/settings", "Authorization", "", "Cookie", "session="+operatorToken); seen.Kind != actor.KindAnonymous {
		t.Errorf("token in a cookie: actor %+v, want anonymous", *seen)
	}
}

func TestOperatorRefusesRemotePeers(t *testing.T) {
	console, err := devconsole.New(operatorToken)
	if err != nil {
		t.Fatal(err)
	}
	var seen actor.Actor
	h := console.Operator("/ops/", actor.System("dev-console"), nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = actor.FromOrAnonymous(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/ops/settings", nil)
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	req.RemoteAddr = "10.0.0.7:4242"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen.Kind != actor.KindAnonymous {
		t.Errorf("remote peer: actor %+v, want anonymous", seen)
	}
}

func TestOperatorOnNilConsole(t *testing.T) {
	var console *devconsole.Console
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := actor.From(r.Context()); ok {
			t.Error("a nil console set an actor")
		}
	})
	h := console.Operator("/ops/", actor.System("dev-console"), nil)(next)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/ops/settings", nil)
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Error("next wasn't called")
	}
}

// TestOperatorRefusesTunnelledRequests: the dev operator on /ops/ must not
// be reachable through orb dev's tunnel (ADR-0086), whether the app trusts
// loopback as a proxy (APP_TRUSTED_PROXIES, which then makes the visitor the
// peer) or not, and whatever Host cloudflared forwards.
func TestOperatorRefusesTunnelledRequests(t *testing.T) {
	console, err := devconsole.New(operatorToken)
	if err != nil {
		t.Fatal(err)
	}
	var seen actor.Actor
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		seen = actor.FromOrAnonymous(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	operator := actor.Actor{Kind: actor.KindSystem, ID: "dev-console", Permissions: []string{"ops.settings.read"}}
	direct := httptest.NewServer(console.Operator("/ops/", operator, slog.New(slog.DiscardHandler))(next))
	defer direct.Close()
	loopback, _ := httpx.ParseTrustedProxies("127.0.0.1/32,::1/128")
	trusting := httptest.NewServer(httpx.TrustedProxies(loopback)(console.Operator("/ops/", operator, nil)(next)))
	defer trusting.Close()

	for _, srv := range []*httptest.Server{direct, trusting} {
		port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
		for _, host := range []string{"calm-river-demo.trycloudflare.com", "localhost:" + port} {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ops/settings", nil)
			req.Host = host
			req.Header.Set("Authorization", "Bearer "+operatorToken)
			req.Header.Set("Cf-Connecting-Ip", "203.0.113.7")
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			req.Header.Set("Cdn-Loop", "cloudflare")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusNoContent || seen.Kind != actor.KindAnonymous {
				t.Errorf("tunnelled /ops/ with Host %s: %d, actor %+v; want the request through without the operator", host, res.StatusCode, seen)
			}
		}
	}
	// Directly from this machine the token still acts as the operator.
	req, _ := http.NewRequest(http.MethodGet, direct.URL+"/ops/settings", nil)
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if seen.ID != "dev-console" || calls != 5 {
		t.Errorf("direct request: actor %+v after %d calls", seen, calls)
	}
}
