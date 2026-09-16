package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"

	"example.com/acme-api/internal/app"
)

// fromClient sends a request whose connection comes from remote.
func fromClient(t *testing.T, h http.Handler, method, path, body, remote string, headers ...string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Add(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r := response{code: rec.Code, header: rec.Header(), body: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.json)
	return r
}

// TestSignInLimitSharedAcrossInstances signs in with a wrong password,
// alternating between two app instances on one database: together they allow
// auth.login_attempts, not twice as many (ADR-0052).
func TestSignInLimitSharedAcrossInstances(t *testing.T) {
	first, url := newAppWithURL(t, nil)
	second, err := app.New(context.Background(), testConfig(t, map[string]string{"DATABASE_URL": url}))
	if err != nil {
		t.Fatalf("New(second instance) error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })

	instances := []http.Handler{first.Handler(), second.Handler()}
	body := `{"email":"ada@example.com","password":"wrong password here"}`
	for i := range authlib.DefaultLoginAttempts {
		if r := do(t, instances[i%2], "POST", "/v1/auth/login", body); r.code != http.StatusUnauthorized {
			t.Fatalf("attempt %d on instance %d = %d %s, want 401", i+1, i%2+1, r.code, r.body)
		}
	}
	for n, h := range instances {
		if r := do(t, h, "POST", "/v1/auth/login", body); r.code != http.StatusTooManyRequests || r.json["code"] != "too_many_attempts" {
			t.Errorf("attempt past the shared limit on instance %d = %d %s, want 429 too_many_attempts", n+1, r.code, r.body)
		}
	}
}

// TestPerIPLimitBehindTrustedProxy checks that clients behind one trusted
// load balancer have their own budgets, and that a client which isn't a
// trusted proxy can't choose its address to escape its limit.
func TestPerIPLimitBehindTrustedProxy(t *testing.T) {
	h := newApp(t, map[string]string{"APP_TRUSTED_PROXIES": "10.0.0.0/8"}).Handler()
	const path, body, proxy = "/v1/auth/password/forgot", `{"email":"nobody@example.com"}`, "10.0.0.5:443"

	for i := range 60 {
		if r := fromClient(t, h, "POST", path, body, proxy, "X-Forwarded-For", "198.51.100.1"); r.code != http.StatusAccepted {
			t.Fatalf("request %d through the proxy = %d %s, want 202", i+1, r.code, r.body)
		}
	}
	if r := fromClient(t, h, "POST", path, body, proxy, "X-Forwarded-For", "198.51.100.1"); r.code != http.StatusTooManyRequests {
		t.Errorf("request 61 from one client = %d %s, want 429", r.code, r.body)
	}
	if r := fromClient(t, h, "POST", path, body, proxy, "X-Forwarded-For", "198.51.100.2"); r.code != http.StatusAccepted {
		t.Errorf("another client behind the same proxy = %d %s, want 202", r.code, r.body)
	}

	const direct = "203.0.113.9:5555"
	for i := range 60 {
		fromClient(t, h, "POST", path, body, direct, "X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i))
	}
	if r := fromClient(t, h, "POST", path, body, direct, "X-Forwarded-For", "192.0.2.200"); r.code != http.StatusTooManyRequests {
		t.Errorf("request 61 from an untrusted peer claiming new addresses = %d %s, want 429", r.code, r.body)
	}
}

func TestTrustedProxiesConfiguration(t *testing.T) {
	for value, wantErr := range map[string]bool{"": false, "10.0.0.0/8, 192.0.2.10": false, "0.0.0.0/0": true, "load-balancer": true} {
		_, err := app.LoadConfig(config.Source{Getenv: func(k string) string {
			switch k {
			case "APP_TRUSTED_PROXIES":
				return value
			case "APP_ENV":
				return "development"
			case "AUTH_ENCRYPTION_KEYS":
				return testEncryptionKeys
			}
			return ""
		}, ReadFile: os.ReadFile})
		if gotErr := err != nil && strings.Contains(err.Error(), "APP_TRUSTED_PROXIES"); gotErr != wantErr {
			t.Errorf("LoadConfig(APP_TRUSTED_PROXIES=%q) error = %v, want error: %v", value, err, wantErr)
		}
	}
}
