package authhttp

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
)

// TestSignInLimitSharedAcrossInstances signs in with a wrong password,
// alternating between two app instances on one database: together they allow
// auth.login_attempts, not twice as many (ADR-0052).
func TestSignInLimitSharedAcrossInstances(t *testing.T) {
	first, url := newAppWithURL(t, nil)
	// Migrations are idempotent, so the second instance's are a no-op.
	second := buildApp(t, testConfig(t, map[string]string{"DATABASE_URL": url}))

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
	// Checking a code for an address without an account is quick, so 61
	// requests fit well within the minute. Each uses another address: codes
	// have their own limit per address.
	const path, proxy = "/v1/auth/verify-email", "10.0.0.5:443"
	n := 0
	body := func() string {
		n++
		return fmt.Sprintf(`{"email":"nobody%d@example.com","code":"000000"}`, n)
	}

	for i := range 60 {
		if r := fromClient(t, h, "POST", path, body(), proxy, "X-Forwarded-For", "198.51.100.1"); r.code != http.StatusUnprocessableEntity {
			t.Fatalf("request %d through the proxy = %d %s, want 422", i+1, r.code, r.body)
		}
	}
	if r := fromClient(t, h, "POST", path, body(), proxy, "X-Forwarded-For", "198.51.100.1"); r.code != http.StatusTooManyRequests {
		t.Errorf("request 61 from one client = %d %s, want 429", r.code, r.body)
	}
	if r := fromClient(t, h, "POST", path, body(), proxy, "X-Forwarded-For", "198.51.100.2"); r.code != http.StatusUnprocessableEntity {
		t.Errorf("another client behind the same proxy = %d %s, want 422", r.code, r.body)
	}

	const direct = "203.0.113.9:5555"
	for i := range 60 {
		fromClient(t, h, "POST", path, body(), direct, "X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i))
	}
	if r := fromClient(t, h, "POST", path, body(), direct, "X-Forwarded-For", "192.0.2.200"); r.code != http.StatusTooManyRequests {
		t.Errorf("request 61 from an untrusted peer claiming new addresses = %d %s, want 429", r.code, r.body)
	}
}

// TestPerIPLimitGroupsIPv6 checks that every address of one IPv6 /64, which
// a single host or customer usually holds, shares one per-IP budget
// (security review AUTH-S-7, OPS-1).
func TestPerIPLimitGroupsIPv6(t *testing.T) {
	h := newApp(t, nil).Handler()
	verify := func(i int, remote string) response {
		return fromClient(t, h, "POST", "/v1/auth/verify-email", fmt.Sprintf(`{"email":"nobody%d@example.com","code":"000000"}`, i), remote)
	}
	for i := range 60 {
		if r := verify(i, fmt.Sprintf("[2001:db8:1:2::%x]:443", i+1)); r.code != http.StatusUnprocessableEntity {
			t.Fatalf("request %d from a new address of the /64 = %d %s, want 422", i+1, r.code, r.body)
		}
	}
	if r := verify(60, "[2001:db8:1:2:ffff:ffff:ffff:1]:443"); r.code != http.StatusTooManyRequests || r.json["code"] != "rate_limited" {
		t.Errorf("request 61 from the same /64 = %d %s, want 429 rate_limited", r.code, r.body)
	}
	if r := verify(61, "[2001:db8:1:3::1]:443"); r.code != http.StatusUnprocessableEntity {
		t.Errorf("request from another /64 = %d %s, want 422", r.code, r.body)
	}
}

func TestTrustedProxiesConfiguration(t *testing.T) {
	for value, wantErr := range map[string]bool{"": false, "10.0.0.0/8, 192.0.2.10": false, "0.0.0.0/0": true, "load-balancer": true} {
		_, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string {
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
