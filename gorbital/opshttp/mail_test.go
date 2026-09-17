package opshttp_test

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

	"gorbital.dev/gorbital/internal/opstest"
)

// This test is TestEmailThroughOps from a v0.1 golden app's
// internal/app/mail_test.go, run against the library module. The file's
// TestEmailConfiguration checks the app's LoadConfig, not /ops/mail, so it
// isn't here.

// Mailpit from the repository's compose.yaml. Without these variables, the
// tests check that email is queued but not that it arrives.
const (
	envMailpitSMTP = "GORBITAL_TEST_MAILPIT_SMTP"
	envMailpitURL  = "GORBITAL_TEST_MAILPIT_URL"
)

// mailProviderName and mailProviderDetails are what GET /ops/mail reports
// about the provider without its configuration: the module's default
// provider, Resend.
var (
	mailProviderName    = "resend"
	mailProviderDetails = map[string]any{"api_key": "missing", "webhook_secret": "missing"}
)

func TestEmailThroughOps(t *testing.T) {
	// The golden app's development default was Mailpit; the harness's is
	// devmail, so the test asks for Mailpit as the golden app had it.
	env := map[string]string{"MAIL_DELIVERY": "mailpit"}
	mailpitAPI := os.Getenv(envMailpitURL)
	if addr := os.Getenv(envMailpitSMTP); addr != "" {
		env["MAILPIT_SMTP_ADDR"] = addr
		env["MAIL_DELIVERY"] = "mailpit"
	}
	a := opstest.New(t, opstest.Options{Env: env})
	a.StartWorkers(t)
	h := a.Handler()
	bearer, _ := a.SignIn(t, "admin@example.com", "platform_admin")

	status := opstest.Do(t, h, "GET", "/ops/mail", "", bearer...)
	details, _ := status.JSON["details"].(map[string]any)
	if status.Code != http.StatusOK || status.JSON["provider"] != mailProviderName || status.JSON["delivery"] != "mailpit" ||
		status.JSON["from_email"] != "no-reply@example.com" || status.JSON["from_name"] != "acme-api" || !maps.Equal(details, mailProviderDetails) {
		t.Fatalf("GET /ops/mail = %d %s", status.Code, status.Body)
	}

	// Who emails appear to come from needs a reason (security review OPS-9).
	if r := opstest.Do(t, h, "PUT", "/ops/settings/mail.reply_to", `{"value":"attacker@evil.test","version":0}`, bearer...); r.Code != 422 || r.JSON["code"] != "setting_reason_required" {
		t.Errorf("PUT mail.reply_to without reason = %d %s, want 422 setting_reason_required", r.Code, r.Body)
	}
	for key, value := range map[string]string{"mail.from_email": "hello@acme.test", "mail.from_name": "Acme", "mail.reply_to": "support@acme.test"} {
		if r := opstest.Do(t, h, "PUT", "/ops/settings/"+key, fmt.Sprintf(`{"value":%q,"version":0,"reason":"our sender"}`, value), bearer...); r.Code != http.StatusOK {
			t.Fatalf("PUT /ops/settings/%s = %d %s", key, r.Code, r.Body)
		}
	}
	if r := opstest.Do(t, h, "PUT", "/ops/settings/mail.from_email", `{"value":"not an email","version":1}`, bearer...); r.Code != 422 {
		t.Errorf("invalid sender address = %d %s, want 422", r.Code, r.Body)
	}
	if r := opstest.Do(t, h, "GET", "/ops/mail", "", bearer...); r.JSON["from_email"] != "hello@acme.test" || r.JSON["reply_to"] != "support@acme.test" {
		t.Errorf("GET /ops/mail after changing the sender = %s", r.Body)
	}

	to := fmt.Sprintf("ops-%d@example.com", time.Now().UnixNano())
	sent := opstest.Do(t, h, "POST", "/ops/mail/test", fmt.Sprintf(`{"to":%q}`, to), bearer...)
	if sent.Code != http.StatusAccepted || sent.JSON["status"] != "queued" || sent.JSON["to"] != to {
		t.Fatalf("POST /ops/mail/test = %d %s", sent.Code, sent.Body)
	}
	if r := opstest.Do(t, h, "POST", "/ops/mail/test", `{"to":"nobody"}`, bearer...); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("POST /ops/mail/test with an invalid address = %d %s, want 422", r.Code, r.Body)
	}
	events := opstest.Do(t, h, "GET", "/ops/audit?action=mail.test.requested", "", bearer...)
	if list, _ := events.JSON["events"].([]any); len(list) != 1 || strings.Contains(events.Body, to) {
		t.Errorf("GET /ops/audit?action=mail.test.requested = %s, want one event without the recipient", events.Body)
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
	opstest.WaitFor(t, "the test email in Mailpit", func() bool {
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
