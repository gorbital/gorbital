package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"apistock.dev/config"

	"example.com/acme-api/internal/app"
)

// Mailpit from the repository's compose.yaml. Without these variables, the
// tests check that email is queued but not that it arrives.
const (
	envMailpitSMTP = "APISTOCK_TEST_MAILPIT_SMTP"
	envMailpitURL  = "APISTOCK_TEST_MAILPIT_URL"
)

func TestEmailThroughOps(t *testing.T) {
	env := map[string]string{"OPS_TOKEN": opsToken}
	mailpitAPI := os.Getenv(envMailpitURL)
	if addr := os.Getenv(envMailpitSMTP); addr != "" {
		env["MAILPIT_SMTP_ADDR"] = addr
	}
	a := newApp(t, env)
	startWorkers(t, a)
	h := a.Handler()

	status := do(t, h, "GET", "/ops/mail", "", bearer...)
	details, _ := status.json["details"].(map[string]any)
	if status.code != http.StatusOK || status.json["provider"] != "resend" || status.json["delivery"] != "mailpit" ||
		status.json["from_email"] != "no-reply@example.com" || status.json["from_name"] != "acme-api" || details["api_key"] != "missing" {
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
		_, err := app.LoadConfig(config.Source{Getenv: func(k string) string { return env[k] }, ReadFile: os.ReadFile})
		return err
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"development uses Mailpit without a key", map[string]string{}, ""},
		{"production needs the Resend key", map[string]string{"APP_ENV": "production"}, "RESEND_API_KEY"},
		{"production with the key", map[string]string{"APP_ENV": "production", "RESEND_API_KEY": "re_123"}, ""},
		{"production never uses Mailpit", map[string]string{"APP_ENV": "production", "RESEND_API_KEY": "re_123", "MAIL_DELIVERY": "mailpit"}, "MAIL_DELIVERY"},
		{"real email in development needs the key", map[string]string{"MAIL_DELIVERY": "provider"}, "RESEND_API_KEY"},
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
