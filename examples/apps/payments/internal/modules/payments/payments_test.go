package payments_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/webhook"

	"example.com/payments/db/migrations"
	"example.com/payments/internal/modules/payments"
	"example.com/payments/internal/modules/payments/domain"
	"example.com/payments/internal/modules/payments/repository"
	"example.com/payments/internal/modules/payments/usecase"
)

// docs:start test-signing

// The provider's signing secret in the tests: "whsec_" and base64, as the
// dashboard shows it. testKey is the same bytes, which the tests sign with.
var (
	testKey    = []byte("a-test-signing-secret-of-32-bytes")
	testSecret = webhook.StandardSecretPrefix + base64.StdEncoding.EncodeToString(testKey)
)

// sign returns the Standard Webhooks headers for a delivery: the event ID,
// the time it was signed, and HMAC-SHA256 of "<id>.<timestamp>.<body>".
// This is what the provider's library does.
func sign(id string, at time.Time, body string) http.Header {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, testKey)
	fmt.Fprintf(mac, "%s.%s.%s", id, timestamp, body)
	return http.Header{
		"Webhook-Id":        {id},
		"Webhook-Timestamp": {timestamp},
		"Webhook-Signature": {"v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))},
	}
}

// docs:end test-signing

// newApp builds the billing API with the payments module on a new database
// for the test, with the provider's secret in the environment.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	t.Setenv(payments.WebhookSecretVar, testSecret)
	return gorbitaltest.New(t,
		gorbital.WithModules(payments.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// deliver posts body to the webhook with header, as the provider does.
func deliver(app *gorbitaltest.App, header http.Header, body string) *gorbitaltest.Response {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/payments", strings.NewReader(body))
	req.Header = header.Clone()
	req.Header.Set("Content-Type", "application/json")
	return app.Client().Do(req)
}

// succeeded is a delivery body for a payment of £49.99.
func succeeded(paymentID string) string {
	return `{"type":"payment.succeeded","data":{"payment_id":"` + paymentID +
		`","amount_minor":4999,"currency":"GBP","occurred_at":"2026-09-23T10:00:00Z"}}`
}

// result is what the webhook answers.
type result struct {
	Applied   bool   `json:"applied"`
	PaymentID string `json:"payment_id"`
}

// counts returns how many payments are recorded and how many receipt jobs
// are queued.
func counts(t *testing.T, app *gorbitaltest.App) (rows, jobs int) {
	t.Helper()
	if err := app.App().Deps().DB.QueryRow(context.Background(), `SELECT count(*) FROM payments`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows, len(app.Jobs(t, usecase.ReceiptJob))
}

// docs:start test-records

// TestSignedDeliveryIsRecorded: a delivery the provider signed is recorded,
// its receipt is queued in the same transaction, and staff can read the
// payment back.
func TestSignedDeliveryIsRecorded(t *testing.T) {
	app := newApp(t)
	body := succeeded("pi_3Qabc")

	res := deliver(app, sign("evt_1", time.Now(), body), body)
	res.AssertStatus(t, http.StatusOK)
	var got result
	res.JSON(t, &got)
	if !got.Applied || got.PaymentID == "" {
		t.Fatalf("POST /v1/webhooks/payments = %+v, want a recorded payment", got)
	}
	if rows, jobs := counts(t, app); rows != 1 || jobs != 1 {
		t.Errorf("after one delivery: %d payments, %d receipt jobs; want 1 and 1", rows, jobs)
	}

	// The job names the payment, not its amount: the worker reads the row
	// the transaction committed.
	if args := string(app.Jobs(t, usecase.ReceiptJob)[0].Args); !strings.Contains(args, got.PaymentID) {
		t.Errorf("receipt job args = %s, want the payment ID", args)
	}

	var payment struct {
		ID          string `json:"id"`
		EventID     string `json:"event_id"`
		Currency    string `json:"currency"`
		Status      string `json:"status"`
		AmountMinor int64  `json:"amount_minor"`
	}
	staff := app.As(gorbitaltest.User("usr_staff", usecase.PermRead))
	staff.Get("/v1/payments/"+got.PaymentID).JSON(t, &payment)
	if payment.EventID != "evt_1" || payment.AmountMinor != 4999 || payment.Currency != "GBP" || payment.Status != "succeeded" {
		t.Errorf("GET /v1/payments/{id} = %+v, want evt_1, 4999 GBP, succeeded", payment)
	}
	// Reading needs the permission; the webhook itself is public.
	app.Client().Get("/v1/payments/"+got.PaymentID).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	staff.Get("/v1/payments/pay_missing").AssertProblem(t, http.StatusNotFound, "payment_not_found")
}

// docs:end test-records

// docs:start test-replay

// TestReplayedDeliveryIsRecordedOnce: the provider retries a delivery it
// isn't sure arrived. The retry carries the same event ID and a fresh
// timestamp and signature, so it verifies; the event ID is what stops it
// being recorded twice.
func TestReplayedDeliveryIsRecordedOnce(t *testing.T) {
	app := newApp(t)
	body := succeeded("pi_3Qabc")

	first := deliver(app, sign("evt_1", time.Now(), body), body)
	first.AssertStatus(t, http.StatusOK)
	var recorded result
	first.JSON(t, &recorded)

	// The retry, signed again a minute later.
	retry := deliver(app, sign("evt_1", time.Now().Add(time.Minute), body), body)
	retry.AssertStatus(t, http.StatusOK)
	var replayed result
	retry.JSON(t, &replayed)
	if replayed.Applied || replayed.PaymentID != recorded.PaymentID {
		t.Errorf("retry = %+v, want applied false and %s", replayed, recorded.PaymentID)
	}
	if rows, jobs := counts(t, app); rows != 1 || jobs != 1 {
		t.Errorf("after the retry: %d payments, %d receipt jobs; want 1 and 1", rows, jobs)
	}

	// A second event about the same payment is a different delivery, and is
	// recorded: the refund of what was paid.
	refund := `{"type":"payment.refunded","data":{"payment_id":"pi_3Qabc","amount_minor":4999,"currency":"GBP","occurred_at":"2026-09-24T10:00:00Z"}}`
	deliver(app, sign("evt_2", time.Now(), refund), refund).AssertStatus(t, http.StatusOK)
	if rows, jobs := counts(t, app); rows != 2 || jobs != 2 {
		t.Errorf("after the refund: %d payments, %d receipt jobs; want 2 and 2", rows, jobs)
	}
}

// docs:end test-replay

// docs:start test-signature

// TestUnauthenticDeliveriesAreRefused: a body signed with another secret, a
// body changed after it was signed, a delivery with no headers at all, and
// one signed outside the replay window all get the same 401, and none of
// them writes anything.
func TestUnauthenticDeliveriesAreRefused(t *testing.T) {
	app := newApp(t)
	body := succeeded("pi_3Qabc")

	wrongKey := hmac.New(sha256.New, []byte("another-signing-secret-32-bytes."))
	fmt.Fprintf(wrongKey, "evt_1.%d.%s", time.Now().Unix(), body)
	forged := sign("evt_1", time.Now(), body)
	forged.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(wrongKey.Sum(nil)))

	// The body the handler would have seen, changed after signing.
	tampered := sign("evt_1", time.Now(), body)

	for name, req := range map[string]struct {
		header http.Header
		body   string
	}{
		"wrong secret":     {forged, body},
		"changed body":     {tampered, succeeded("pi_attacker")},
		"no signature":     {http.Header{}, body},
		"outside window":   {sign("evt_1", time.Now().Add(-10*time.Minute), body), body},
		"signed in future": {sign("evt_1", time.Now().Add(10*time.Minute), body), body},
	} {
		t.Run(name, func(t *testing.T) {
			deliver(app, req.header, req.body).AssertProblem(t, http.StatusUnauthorized, "invalid_webhook_signature")
		})
	}
	if rows, jobs := counts(t, app); rows != 0 || jobs != 0 {
		t.Errorf("after five refused deliveries: %d payments, %d receipt jobs; want none", rows, jobs)
	}
}

// docs:end test-signature

// docs:start test-domain

// TestVerifiedButInvalid: a signature proves who sent the delivery, not
// that its contents make sense. An event type the app doesn't record is
// accepted and ignored, so the provider stops retrying it; an amount that
// isn't positive is refused, and leaves no row saying money moved.
func TestVerifiedButInvalid(t *testing.T) {
	app := newApp(t)

	ignored := `{"type":"payout.paid","data":{"payout_id":"po_1"}}`
	res := deliver(app, sign("evt_9", time.Now(), ignored), ignored)
	res.AssertStatus(t, http.StatusOK)
	var got result
	res.JSON(t, &got)
	if got.Applied || got.PaymentID != "" {
		t.Errorf("unknown event type = %+v, want accepted and ignored", got)
	}

	negative := `{"type":"payment.succeeded","data":{"payment_id":"pi_3Qabc","amount_minor":-4999,"currency":"GBP"}}`
	deliver(app, sign("evt_10", time.Now(), negative), negative).
		AssertProblem(t, http.StatusUnprocessableEntity, "invalid_amount")

	noPayment := `{"type":"payment.succeeded","data":{"amount_minor":4999,"currency":"GBP"}}`
	deliver(app, sign("evt_11", time.Now(), noPayment), noPayment).
		AssertProblem(t, http.StatusUnprocessableEntity, "payment_id_required")

	if rows, jobs := counts(t, app); rows != 0 || jobs != 0 {
		t.Errorf("after three deliveries none of which is a payment: %d payments, %d receipt jobs; want none", rows, jobs)
	}
}

// docs:end test-domain

// docs:start test-no-secret

// TestWithoutSecretNothingIsTrusted: a deployment that forgot
// PAYMENTS_WEBHOOK_SECRET refuses every delivery instead of recording
// payments nobody signed. The provider retries, so the deliveries arrive
// once the secret is set.
func TestWithoutSecretNothingIsTrusted(t *testing.T) {
	t.Setenv(payments.WebhookSecretVar, "")
	app := gorbitaltest.New(t,
		gorbital.WithModules(payments.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
	body := succeeded("pi_3Qabc")
	deliver(app, sign("evt_1", time.Now(), body), body).
		AssertProblem(t, http.StatusUnauthorized, "invalid_webhook_signature")
}

// docs:end test-no-secret

// docs:start test-rollback

// TestRollbackTakesTheJobWithIt: the receipt is enqueued with
// jobs.Client.InsertTx, so the job row is written through the same
// transaction as the payment. If the transaction rolls back, neither
// exists: there is no window in which a worker can send a receipt for a
// payment that was never recorded.
func TestRollbackTakesTheJobWithIt(t *testing.T) {
	app := newApp(t)
	deps := app.App().Deps()
	txm := repository.NewTxManager(deps.DB, deps.Jobs)

	payment, err := domain.NewPayment("pay_rolled_back", domain.StatusSucceeded, domain.Event{
		ID: "evt_rolled_back", PaymentID: "pi_3Qabc", AmountMinor: 4999, Currency: "GBP",
	}, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("something after the write failed")
	err = txm.InTx(context.Background(), func(tx usecase.Tx) error {
		if _, _, err := tx.InsertPayment(context.Background(), payment); err != nil {
			return err
		}
		if err := tx.Enqueue(context.Background(), usecase.ReceiptArgs{PaymentID: payment.ID}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("InTx = %v, want the function's own error", err)
	}
	if rows, jobs := counts(t, app); rows != 0 || jobs != 0 {
		t.Errorf("after the rollback: %d payments, %d receipt jobs; want none", rows, jobs)
	}
}

// docs:end test-rollback
