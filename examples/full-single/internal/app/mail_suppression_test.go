package app_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/mail/suppressionpg"
)

// suppress puts email on the app's suppression list directly, as the
// provider's webhook would, and returns its ID.
func suppress(t *testing.T, databaseURL, email string, reason suppressionpg.Reason) int64 {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := suppressionpg.NewStore(pool)
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
	}
	a, url := newAppWithURL(t, env)
	startWorkers(t, a)
	h := a.Handler()
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")

	id := suppress(t, url, "Bounced@Example.com", suppressionpg.ReasonBounce)
	suppress(t, url, "spam@example.com", suppressionpg.ReasonComplaint)

	// The test email goes through the mail worker like every other email.
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"bounced@example.com"}`, admin...); r.code != http.StatusAccepted {
		t.Fatalf("POST /ops/mail/test = %d %s", r.code, r.body)
	}
	run := lastMailJob(t, h, admin)
	errs, _ := run["errors"].([]any)
	if run["state"] != "cancelled" || run["attempt"] != float64(1) || len(errs) != 1 ||
		!strings.Contains(fmt.Sprint(errs[0]), "suppression list") || strings.Contains(fmt.Sprint(run), "bounced@") {
		t.Errorf("email job to a suppressed address = %v, want cancelled after one attempt, saying why without the address", run)
	}

	list := do(t, h, "GET", "/ops/mail/suppressions", "", viewer...)
	items, _ := list.json["suppressions"].([]any)
	if list.code != http.StatusOK || len(items) != 2 {
		t.Fatalf("GET /ops/mail/suppressions as ops_viewer = %d %s", list.code, list.body)
	}
	if first, _ := items[1].(map[string]any); first["email"] != "bounced@example.com" || first["reason"] != "bounce" || first["source"] != "resend" {
		t.Errorf("oldest suppression = %v", first)
	}
	if r := do(t, h, "GET", "/ops/mail/suppressions?reason=complaint&limit=1", "", admin...); len(r.json["suppressions"].([]any)) != 1 || r.json["next_cursor"] != nil {
		t.Errorf("GET /ops/mail/suppressions?reason=complaint = %s", r.body)
	}
	if r := do(t, h, "GET", "/ops/mail/suppressions?cursor=nope", "", admin...); r.code != http.StatusBadRequest || r.json["code"] != "invalid_cursor" {
		t.Errorf("invalid cursor = %d %s", r.code, r.body)
	}

	path := fmt.Sprintf("/ops/mail/suppressions/%d", id)
	if r := do(t, h, "DELETE", path, `{"reason":"mailbox exists again"}`, viewer...); r.code != http.StatusForbidden {
		t.Errorf("DELETE as ops_viewer = %d %s, want 403 (ops.mail.write)", r.code, r.body)
	}
	if r := do(t, h, "DELETE", path, `{"reason":"  "}`, admin...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "mail_suppression_reason_required" {
		t.Errorf("DELETE without a reason = %d %s, want 422 mail_suppression_reason_required", r.code, r.body)
	}
	if r := do(t, h, "DELETE", path, `{"reason":"mailbox exists again"}`, admin...); r.code != http.StatusOK || r.json["email"] != "bounced@example.com" {
		t.Fatalf("DELETE %s = %d %s", path, r.code, r.body)
	}
	if r := do(t, h, "DELETE", path, `{"reason":"mailbox exists again"}`, admin...); r.code != http.StatusNotFound || r.json["code"] != "mail_suppression_not_found" {
		t.Errorf("DELETE twice = %d %s, want 404 mail_suppression_not_found", r.code, r.body)
	}
	events := do(t, h, "GET", "/ops/audit?action=mail.suppression.removed", "", admin...)
	evs, _ := events.json["events"].([]any)
	if len(evs) != 1 || !strings.Contains(events.body, "mailbox exists again") || strings.Contains(events.body, "bounced@") {
		t.Errorf("GET /ops/audit?action=mail.suppression.removed = %s, want one event with the reason and without the address", events.body)
	}

	// Once removed, the address receives email again.
	if env["MAILPIT_SMTP_ADDR"] == "" {
		t.Logf("set %s to check that the address receives email again", envMailpitSMTP)
		return
	}
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"bounced@example.com"}`, admin...); r.code != http.StatusAccepted {
		t.Fatalf("POST /ops/mail/test = %d %s", r.code, r.body)
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
		jobs, _ := do(t, h, "GET", "/ops/jobs/runs?kind=gorbital.mail.send&limit=1", "", bearer...).json["jobs"].([]any)
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
	a := newApp(t, nil)
	r := do(t, a.Handler(), "POST", "/v1/webhooks/resend", `{"type":"email.bounced"}`,
		"svix-id", "msg_1", "svix-timestamp", "1789552800", "svix-signature", "v1,AAAA")
	if r.code != http.StatusNotFound || r.json["code"] != "webhook_not_found" {
		t.Errorf("POST /v1/webhooks/resend without a secret = %d %s, want 404 webhook_not_found", r.code, r.body)
	}
}
