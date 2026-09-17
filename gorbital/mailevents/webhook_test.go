package mailevents_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/postgres/pgtest"
)

// These tests are a v0.1 golden app's internal/app/mail_suppression_test.go
// and the webhook parts of internal/app/infra_mail_test.go, run against the
// library modules: mailevents serves the webhook and opshttp the
// suppression list.

// Mailpit from the repository's compose.yaml. Without this variable, the
// tests check that email is queued but not that it arrives.
const envMailpitSMTP = "GORBITAL_TEST_MAILPIT_SMTP"

// suppress puts email on the app's suppression list directly, as the
// provider's webhook would, and returns its ID.
func suppress(t *testing.T, a *testApp, email string, reason suppressionpg.Reason) int64 {
	t.Helper()
	// The golden test opened its own pool on the database URL; the app's
	// pool is the same database.
	store, err := suppressionpg.NewStore(a.Deps().DB)
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.Add(context.Background(), suppressionpg.Entry{Email: email, Reason: reason, Source: "resend", Detail: "Permanent/General"})
	if err != nil || len(added) != 1 {
		t.Fatalf("Add(%s) = %+v, %v", email, added, err)
	}
	return added[0].ID
}

// TestSuppressedAddressesGetNoEmail checks the suppression list with any
// provider (ADR-0062): a suppressed recipient's email job is cancelled
// without a retry, and operators list and remove suppressions with a reason.
func TestSuppressedAddressesGetNoEmail(t *testing.T) {
	env := map[string]string{}
	if addr := os.Getenv(envMailpitSMTP); addr != "" {
		env["MAILPIT_SMTP_ADDR"] = addr
		env["MAIL_DELIVERY"] = "mailpit"
	}
	a := newTestApp(t, testAppOptions{Env: env})
	a.StartWorkers(t)
	h := a.Handler()
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")

	id := suppress(t, a, "Bounced@Example.com", suppressionpg.ReasonBounce)
	suppress(t, a, "spam@example.com", suppressionpg.ReasonComplaint)

	// The test email goes through the mail worker like every other email.
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"bounced@example.com"}`, admin...); r.Code != http.StatusAccepted {
		t.Fatalf("POST /ops/mail/test = %d %s", r.Code, r.Body)
	}
	run := lastMailJob(t, h, admin)
	errs, _ := run["errors"].([]any)
	if run["state"] != "cancelled" || run["attempt"] != float64(1) || len(errs) != 1 ||
		!strings.Contains(fmt.Sprint(errs[0]), "suppression list") || strings.Contains(fmt.Sprint(run), "bounced@") {
		t.Errorf("email job to a suppressed address = %v, want cancelled after one attempt, saying why without the address", run)
	}

	list := do(t, h, "GET", "/ops/mail/suppressions", "", viewer...)
	items, _ := list.JSON["suppressions"].([]any)
	if list.Code != http.StatusOK || len(items) != 2 {
		t.Fatalf("GET /ops/mail/suppressions as ops_viewer = %d %s", list.Code, list.Body)
	}
	if first, _ := items[1].(map[string]any); first["email"] != "bounced@example.com" || first["reason"] != "bounce" || first["source"] != "resend" {
		t.Errorf("oldest suppression = %v", first)
	}
	if r := do(t, h, "GET", "/ops/mail/suppressions?reason=complaint&limit=1", "", admin...); len(r.JSON["suppressions"].([]any)) != 1 || r.JSON["next_cursor"] != nil {
		t.Errorf("GET /ops/mail/suppressions?reason=complaint = %s", r.Body)
	}
	if r := do(t, h, "GET", "/ops/mail/suppressions?cursor=nope", "", admin...); r.Code != http.StatusBadRequest || r.JSON["code"] != "invalid_cursor" {
		t.Errorf("invalid cursor = %d %s", r.Code, r.Body)
	}

	path := fmt.Sprintf("/ops/mail/suppressions/%d", id)
	if r := do(t, h, "DELETE", path, `{"reason":"mailbox exists again"}`, viewer...); r.Code != http.StatusForbidden {
		t.Errorf("DELETE as ops_viewer = %d %s, want 403 (ops.mail.write)", r.Code, r.Body)
	}
	if r := do(t, h, "DELETE", path, `{"reason":"  "}`, admin...); r.Code != http.StatusUnprocessableEntity || r.JSON["code"] != "mail_suppression_reason_required" {
		t.Errorf("DELETE without a reason = %d %s, want 422 mail_suppression_reason_required", r.Code, r.Body)
	}
	if r := do(t, h, "DELETE", path, `{"reason":"mailbox exists again"}`, admin...); r.Code != http.StatusOK || r.JSON["email"] != "bounced@example.com" {
		t.Fatalf("DELETE %s = %d %s", path, r.Code, r.Body)
	}
	if r := do(t, h, "DELETE", path, `{"reason":"mailbox exists again"}`, admin...); r.Code != http.StatusNotFound || r.JSON["code"] != "mail_suppression_not_found" {
		t.Errorf("DELETE twice = %d %s, want 404 mail_suppression_not_found", r.Code, r.Body)
	}
	events := do(t, h, "GET", "/ops/audit?action=mail.suppression.removed", "", admin...)
	evs, _ := events.JSON["events"].([]any)
	if len(evs) != 1 || !strings.Contains(events.Body, "mailbox exists again") || strings.Contains(events.Body, "bounced@") {
		t.Errorf("GET /ops/audit?action=mail.suppression.removed = %s, want one event with the reason and without the address", events.Body)
	}

	// Once removed, the address receives email again.
	if env["MAILPIT_SMTP_ADDR"] == "" {
		t.Logf("set %s to check that the address receives email again", envMailpitSMTP)
		return
	}
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"bounced@example.com"}`, admin...); r.Code != http.StatusAccepted {
		t.Fatalf("POST /ops/mail/test = %d %s", r.Code, r.Body)
	}
	if run := lastMailJob(t, h, admin); run["state"] != "completed" {
		t.Errorf("email job after removing the suppression = %v, want completed", run)
	}
}

// lastMailJob waits for the newest email job, the one the request just
// queued, to finish and returns it.
func lastMailJob(t *testing.T, h http.Handler, bearer []string) map[string]any {
	t.Helper()
	var run map[string]any
	waitFor(t, "the email job to finish", func() bool {
		jobs, _ := do(t, h, "GET", "/ops/jobs/runs?kind=gorbital.mail.send&limit=1", "", bearer...).JSON["jobs"].([]any)
		if len(jobs) != 1 {
			return false
		}
		run, _ = jobs[0].(map[string]any)
		return run["finalized_at"] != nil
	})
	return run
}

// TestWebhookNeedsItsSecret checks that the provider webhook answers 404
// without RESEND_WEBHOOK_SECRET, whatever the provider.
func TestWebhookNeedsItsSecret(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	r := do(t, a.Handler(), "POST", "/v1/webhooks/resend", `{"type":"email.bounced"}`,
		"svix-id", "msg_1", "svix-timestamp", "1789552800", "svix-signature", "v1,AAAA")
	if r.Code != http.StatusNotFound || r.JSON["code"] != "webhook_not_found" {
		t.Errorf("POST /v1/webhooks/resend without a secret = %d %s, want 404 webhook_not_found", r.Code, r.Body)
	}
}

// testConfig loads a development configuration on a new database with env
// on top, as newTestApp does.
func testConfig(t *testing.T, env map[string]string) gorbital.Config {
	t.Helper()
	full := map[string]string{
		"APP_ENV": "development", "APP_ADDR": "127.0.0.1:0", "DATABASE_URL": pgtest.NewDatabase(t), "APP_DB_MAX_CONNS": "8", "APP_JOB_WORKERS": "4",
		"LOG_ARCHIVE_DIR": t.TempDir(), "STORAGE_LOCAL_DIR": t.TempDir(),
	}
	maps.Copy(full, env)
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string { return full[k] }, ReadFile: os.ReadFile})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return cfg
}

// TestMailProviderConfiguration is the RESEND_WEBHOOK_SECRET part of the
// golden test. The v0.1 app checked the secret in LoadConfig; the library's
// LoadConfig doesn't know the mailevents module, which checks it when the
// app is built, so gorbital.New fails instead. Its configuration error is
// unexported, so the test checks the message. The RESEND_API_KEY checks
// belong to gorbital's configuration, not to this module.
func TestMailProviderConfiguration(t *testing.T) {
	if _, err := buildTestApp(t, testConfig(t, map[string]string{"RESEND_WEBHOOK_SECRET": testWebhookSecret}), testAppOptions{}); err != nil {
		t.Errorf("New() with RESEND_WEBHOOK_SECRET error = %v", err)
	}
	_, err := buildTestApp(t, testConfig(t, map[string]string{"RESEND_WEBHOOK_SECRET": "whsec_not-base64!"}), testAppOptions{})
	if err == nil || !strings.Contains(err.Error(), "RESEND_WEBHOOK_SECRET") || strings.Contains(err.Error(), "not-base64") {
		t.Errorf("New() with a malformed RESEND_WEBHOOK_SECRET error = %v, want one naming the variable without quoting it", err)
	}
}

// TestInvalidWebhookSecretFailsNew checks that an app whose
// RESEND_WEBHOOK_SECRET isn't a Resend signing secret doesn't start: a
// secret that isn't base64, too short or empty after whsec_ is a
// configuration error rather than a webhook that refuses every request.
func TestInvalidWebhookSecretFailsNew(t *testing.T) {
	for name, secret := range map[string]string{
		"not base64":  "whsec_%%%%",
		"too short":   "whsec_" + base64.StdEncoding.EncodeToString([]byte("short")),
		"only prefix": "whsec_",
	} {
		a, err := buildTestApp(t, testConfig(t, map[string]string{"RESEND_WEBHOOK_SECRET": secret}), testAppOptions{})
		if err == nil || a != nil || !strings.Contains(err.Error(), "RESEND_WEBHOOK_SECRET") {
			t.Errorf("%s: New() error = %v, want a configuration error naming RESEND_WEBHOOK_SECRET", name, err)
		}
	}
}

// testWebhookSecret is a Resend webhook signing secret: whsec_ and 24 bytes
// in base64.
const testWebhookSecret = "whsec_AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcY" // gitleaks:allow (test value)

// signWebhook returns the Svix headers for body, signed at signedAt with
// secret, computed here with crypto/hmac rather than the resend module.
func signWebhook(secret, id, body string, signedAt time.Time) []string {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	ts := strconv.FormatInt(signedAt.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	return []string{"svix-id", id, "svix-timestamp", ts, "svix-signature", "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))}
}

func bounceEvent(to, bounceType string) string {
	return fmt.Sprintf(`{"type":"email.bounced","created_at":"2026-09-16T10:00:00.000Z","data":{"email_id":"56761188-7520-42d8-8898-ff6fc54ce618",`+
		`"from":"Acme <no-reply@acme.test>","to":[%q],"subject":"Welcome","bounce":{"message":"550 5.1.1 <%s>: Recipient address rejected","subType":"General","type":%q}}}`,
		to, to, bounceType)
}

// TestResendWebhook checks POST /v1/webhooks/resend end to end (ADR-0062).
func TestResendWebhook(t *testing.T) {
	a := newTestApp(t, testAppOptions{Env: map[string]string{"RESEND_WEBHOOK_SECRET": testWebhookSecret}})
	h := a.Handler()
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")
	now := time.Now()
	post := func(body string, headers ...string) response {
		return do(t, h, "POST", "/v1/webhooks/resend", body, headers...)
	}
	suppressed := func() map[string]string {
		list, _ := do(t, h, "GET", "/ops/mail/suppressions", "", admin...).JSON["suppressions"].([]any)
		out := map[string]string{}
		for _, item := range list {
			s, _ := item.(map[string]any)
			out[fmt.Sprint(s["email"])] = fmt.Sprintf("%v %v", s["reason"], s["detail"])
		}
		return out
	}

	// A hard bounce suppresses the recipient. The request carries no Origin
	// or Sec-Fetch-Site header, as from Resend's servers, so cross-origin
	// protection lets it through.
	hard := bounceEvent("Hard@Example.com", "Permanent")
	hardHeaders := signWebhook(testWebhookSecret, "msg_hard", hard, now)
	if r := post(hard, hardHeaders...); r.Code != http.StatusNoContent {
		t.Fatalf("hard bounce = %d %s, want 204", r.Code, r.Body)
	}
	if got := suppressed(); got["hard@example.com"] != "bounce Permanent/General" || len(got) != 1 {
		t.Errorf("suppressions after a hard bounce = %v", got)
	}

	// Soft bounces, deliveries and unknown events are accepted and ignored.
	for id, body := range map[string]string{
		"msg_soft":         bounceEvent("soft@example.com", "Transient"),
		"msg_undetermined": bounceEvent("maybe@example.com", "Undetermined"),
		"msg_delivered":    `{"type":"email.delivered","data":{"to":["ok@example.com"]}}`,
		"msg_opened":       `{"type":"email.opened","data":{"to":["ok@example.com"]}}`,
	} {
		if r := post(body, signWebhook(testWebhookSecret, id, body, now)...); r.Code != http.StatusNoContent {
			t.Errorf("%s = %d %s, want 204", id, r.Code, r.Body)
		}
	}

	// A complaint suppresses; several signatures (a rotated secret) work.
	complaint := `{"type":"email.complained","created_at":"2026-09-16T10:00:00Z","data":{"to":["spam@example.com"]}}`
	headers := signWebhook(testWebhookSecret, "msg_complaint", complaint, now)
	headers[5] = "v1,c2lnbmVkIHdpdGggdGhlIG9sZCBzZWNyZXQgb2YgMzIgYnl0ZXM= " + headers[5]
	if r := post(complaint, headers...); r.Code != http.StatusNoContent {
		t.Errorf("complaint with two signatures = %d %s, want 204", r.Code, r.Body)
	}
	got := suppressed()
	if len(got) != 2 || got["spam@example.com"] != "complaint <nil>" {
		t.Errorf("suppressions = %v, want the hard bounce and the complaint only", got)
	}

	// Refused: forged, expired, from the future, altered, unsigned.
	for name, tt := range map[string]struct {
		body    string
		headers []string
	}{
		"another secret":   {hard, signWebhook("whsec_ZmFrZSBzZWNyZXQgZm9yIGEgZm9yZ2Vy", "msg_forged", hard, now)},
		"expired":          {hard, signWebhook(testWebhookSecret, "msg_old", hard, now.Add(-6*time.Minute))},
		"from the future":  {hard, signWebhook(testWebhookSecret, "msg_future", hard, now.Add(6*time.Minute))},
		"altered body":     {bounceEvent("victim@example.com", "Permanent"), hardHeaders},
		"no signature":     {hard, nil},
		"signature header": {hard, []string{"svix-id", "msg_x", "svix-timestamp", strconv.FormatInt(now.Unix(), 10), "svix-signature", "v1,AAAA"}},
	} {
		if r := post(tt.body, tt.headers...); r.Code != http.StatusUnauthorized || r.JSON["code"] != "invalid_webhook_signature" {
			t.Errorf("%s = %d %s, want 401 invalid_webhook_signature", name, r.Code, r.Body)
		}
	}
	if len(suppressed()) != 2 {
		t.Errorf("a refused webhook changed the suppression list: %v", suppressed())
	}

	// A signed body that isn't an event.
	if r := post(`not json`, signWebhook(testWebhookSecret, "msg_garbage", `not json`, now)...); r.Code != http.StatusBadRequest || r.JSON["code"] != "invalid_webhook_payload" {
		t.Errorf("signed garbage = %d %s, want 400 invalid_webhook_payload", r.Code, r.Body)
	}
	big := `{"type":"email.delivered","data":{"to":["ok@example.com"],"pad":"` + strings.Repeat("x", 300<<10) + `"}}`
	if r := post(big, signWebhook(testWebhookSecret, "msg_big", big, now)...); r.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("signed body over 256 KiB = %d, want 413", r.Code)
	}

	// Browsers can't post it from another site.
	if r := post(hard, append(hardHeaders, "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.example")...); r.Code != http.StatusForbidden {
		t.Errorf("cross-site browser post = %d %s, want 403", r.Code, r.Body)
	}

	// Removing the address and replaying the captured request within the
	// tolerance doesn't suppress it again.
	list, _ := do(t, h, "GET", "/ops/mail/suppressions", "", admin...).JSON["suppressions"].([]any)
	var hardID any
	for _, item := range list {
		if s, _ := item.(map[string]any); s["email"] == "hard@example.com" {
			hardID = s["id"]
		}
	}
	if r := do(t, h, "DELETE", fmt.Sprintf("/ops/mail/suppressions/%v", hardID), `{"reason":"mailbox created again"}`, admin...); r.Code != http.StatusOK {
		t.Fatalf("DELETE suppression = %d %s", r.Code, r.Body)
	}
	if r := post(hard, hardHeaders...); r.Code != http.StatusNoContent {
		t.Errorf("replayed request = %d %s, want 204 without changes", r.Code, r.Body)
	}
	if _, ok := suppressed()["hard@example.com"]; ok {
		t.Error("a replayed webhook suppressed a removed address again")
	}

	events := do(t, h, "GET", "/ops/audit?action=mail.suppression.added", "", admin...)
	if list, _ := events.JSON["events"].([]any); len(list) != 2 || strings.Contains(events.Body, "@example.com") {
		t.Errorf("GET /ops/audit?action=mail.suppression.added = %s, want 2 events without addresses", events.Body)
	}
}
