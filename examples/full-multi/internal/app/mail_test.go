package app_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"

	"example.com/acme-api/internal/app"
)

// Mailpit from the repository's compose.yaml. Without these variables, the
// tests check that email is queued but not that it arrives.
const (
	envMailpitSMTP = "GORBITAL_TEST_MAILPIT_SMTP"
	envMailpitURL  = "GORBITAL_TEST_MAILPIT_URL"
)

func TestEmailThroughOps(t *testing.T) {
	env := map[string]string{}
	mailpitAPI := os.Getenv(envMailpitURL)
	if addr := os.Getenv(envMailpitSMTP); addr != "" {
		env["MAILPIT_SMTP_ADDR"] = addr
	}
	a := newApp(t, env)
	startWorkers(t, a)
	h := a.Handler()
	bearer, _ := signIn(t, a, "admin@example.com", "platform_admin")

	status := do(t, h, "GET", "/ops/mail", "", bearer...)
	details, _ := status.json["details"].(map[string]any)
	if status.code != http.StatusOK || status.json["provider"] != mailProviderName || status.json["delivery"] != "mailpit" ||
		status.json["from_email"] != "no-reply@example.com" || status.json["from_name"] != "acme-api" || !maps.Equal(details, mailProviderDetails) {
		t.Fatalf("GET /ops/mail = %d %s", status.code, status.body)
	}

	for key, value := range map[string]string{"mail.from_email": "hello@acme.test", "mail.from_name": "Acme", "mail.reply_to": "support@acme.test"} {
		if r := do(t, h, "PUT", "/ops/settings/"+key, fmt.Sprintf(`{"value":%q,"version":0}`, value), bearer...); r.code != http.StatusOK {
			t.Fatalf("PUT /ops/settings/%s = %d %s", key, r.code, r.body)
		}
	}
	if r := do(t, h, "PUT", "/ops/settings/mail.from_email", `{"value":"not an email","version":1}`, bearer...); r.code != 422 {
		t.Errorf("invalid sender address = %d %s, want 422", r.code, r.body)
	}
	if r := do(t, h, "GET", "/ops/mail", "", bearer...); r.json["from_email"] != "hello@acme.test" || r.json["reply_to"] != "support@acme.test" {
		t.Errorf("GET /ops/mail after changing the sender = %s", r.body)
	}

	to := fmt.Sprintf("ops-%d@example.com", time.Now().UnixNano())
	sent := do(t, h, "POST", "/ops/mail/test", fmt.Sprintf(`{"to":%q}`, to), bearer...)
	if sent.code != http.StatusAccepted || sent.json["status"] != "queued" || sent.json["to"] != to {
		t.Fatalf("POST /ops/mail/test = %d %s", sent.code, sent.body)
	}
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"nobody"}`, bearer...); r.code != http.StatusUnprocessableEntity {
		t.Errorf("POST /ops/mail/test with an invalid address = %d %s, want 422", r.code, r.body)
	}
	events := do(t, h, "GET", "/ops/audit?action=mail.test.requested", "", bearer...)
	if list, _ := events.json["events"].([]any); len(list) != 1 || strings.Contains(events.body, to) {
		t.Errorf("GET /ops/audit?action=mail.test.requested = %s, want one event without the recipient", events.body)
	}

	if mailpitAPI == "" {
		t.Logf("set %s and %s to check that the email arrives", envMailpitSMTP, envMailpitURL)
		return
	}
	var message struct {
		Subject string `json:"Subject"`
		From    struct {
			Name    string `json:"Name"`
			Address string `json:"Address"`
		} `json:"From"`
		ReplyTo []struct {
			Address string `json:"Address"`
		} `json:"ReplyTo"`
	}
	waitFor(t, "the test email in Mailpit", func() bool {
		resp, err := http.Get(mailpitAPI + "/api/v1/search?query=" + url.QueryEscape(`to:"`+to+`"`)) //nolint:gosec,noctx // test URL from the environment
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var result struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if json.NewDecoder(resp.Body).Decode(&result) != nil || len(result.Messages) != 1 {
			return false
		}
		return json.Unmarshal(result.Messages[0], &message) == nil
	})
	if message.Subject != "Test email from acme-api" || message.From.Address != "hello@acme.test" || message.From.Name != "Acme" ||
		len(message.ReplyTo) != 1 || message.ReplyTo[0].Address != "support@acme.test" {
		t.Errorf("delivered email = %+v, want the sender from the mail.* settings", message)
	}
}

func TestEmailConfiguration(t *testing.T) {
	load := func(env map[string]string) error {
		getenv := func(k string) string {
			if v, ok := env[k]; ok {
				return v
			}
			if k == "AUTH_ENCRYPTION_KEYS" {
				return testEncryptionKeys
			}
			return ""
		}
		_, err := app.LoadConfig(config.Source{Getenv: getenv, ReadFile: os.ReadFile})
		return err
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"development uses Mailpit without a key", map[string]string{}, ""},
		{"development works without encryption keys", map[string]string{"AUTH_ENCRYPTION_KEYS": ""}, ""},
		{"production needs encryption keys", withMailProvider(map[string]string{"APP_ENV": "production", "AUTH_ENCRYPTION_KEYS": ""}), "AUTH_ENCRYPTION_KEYS"},
		{"production needs the provider's configuration", map[string]string{"APP_ENV": "production"}, mailProviderRequired},
		{"production with the provider configured", withMailProvider(map[string]string{"APP_ENV": "production"}), ""},
		{"production never uses Mailpit", withMailProvider(map[string]string{"APP_ENV": "production", "MAIL_DELIVERY": "mailpit"}), "MAIL_DELIVERY"},
		{"real email in development needs the provider's configuration", map[string]string{"MAIL_DELIVERY": "provider"}, mailProviderRequired},
		{"unknown delivery", map[string]string{"MAIL_DELIVERY": "carrier-pigeon"}, "MAIL_DELIVERY"},
		{"bad Mailpit address", map[string]string{"MAILPIT_SMTP_ADDR": "mailpit"}, "MAILPIT_SMTP_ADDR"},
	}
	for _, tt := range tests {
		err := load(tt.env)
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s: LoadConfig() error = %v", tt.name, err)
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: LoadConfig() error = %v, want one mentioning %s", tt.name, err, tt.wantErr)
		}
	}
}

// withMailProvider returns env plus the variables that configure the email
// provider (infra_mail_test.go).
func withMailProvider(env map[string]string) map[string]string {
	out := maps.Clone(mailProviderEnv)
	maps.Copy(out, env)
	return out
}
