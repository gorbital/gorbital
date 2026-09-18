package payments_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

	"example.com/plateful/db/migrations"
	authhttp "example.com/plateful/internal/modules/auth"
	orgshttp "example.com/plateful/internal/modules/orgs"
	"example.com/plateful/internal/modules/payments"
	"example.com/plateful/internal/modules/payments/usecase"
	"example.com/plateful/internal/modules/restaurants"
)

// These tests drive the payments routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts: a
// customer is an ordinary signed-in user who belongs to no organisation,
// and the restaurant's organisation is that restaurateur's own workspace.
//
// The orders module isn't built into the test app and isn't imported: it is
// being written alongside this one, and a module never imports another
// module's layers anyway. The restaurant and order rows are inserted with
// SQL instead — in the real flow the orders module writes them when a
// customer places an order, and this module only ever reads them.

// The provider's signing secret in the tests: "whsec_" and base64, as a
// provider's dashboard shows it. testKey is the same bytes, which the tests
// sign with.
var (
	testKey    = []byte("a-test-signing-secret-of-32-bytes")
	testSecret = webhook.StandardSecretPrefix + base64.StdEncoding.EncodeToString(testKey)
)

// sign returns the Standard Webhooks headers for a delivery: the event ID,
// the time it was signed, and HMAC-SHA256 of "<id>.<timestamp>.<body>".
// This is what the provider's own library does.
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

// newApp builds Plateful with sign-in, organisations, the restaurants
// module and the payments module, with the provider's secret set.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	t.Setenv(payments.WebhookSecretVar, testSecret)
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), restaurants.Module(), payments.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// signUp creates an account for email and returns its client, its user ID
// and its personal workspace, the organisation every account gets.
func signUp(t *testing.T, app *gorbitaltest.App, email string) (*gorbitaltest.Client, string, string) {
	t.Helper()
	client, userID := app.SignUp(t, email)
	var orgs struct {
		Items []struct {
			ID       string `json:"id"`
			Personal bool   `json:"personal"`
		} `json:"items"`
	}
	client.Get("/v1/orgs").JSON(t, &orgs)
	for _, org := range orgs.Items {
		if org.Personal {
			return client, userID, org.ID
		}
	}
	t.Fatalf("%s has no personal workspace", email)
	return nil, "", ""
}

const (
	insertRestaurantSQL = `
		INSERT INTO restaurants (id, org_id, created_by, name, address, cuisine, delivery_radius_m,
		                         status, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Example address', 'Neapolitan', 3000, 'open', 1, now(), now())
		ON CONFLICT (id) DO NOTHING`
	insertOrderSQL = `
		INSERT INTO orders (id, org_id, restaurant_id, customer_id, status, address, total_minor,
		                    currency, placed_at, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'Example address', $6, 'GBP', now(), 1, now(), now())`
)

// seedOrder writes the restaurant and the order a payment needs, and
// returns the order's ID. The orders module writes both in the real flow;
// this module only ever reads them, with SQL, because a module never
// imports another module's layers.
func seedOrder(t *testing.T, app *gorbitaltest.App, orgID, ownerID, customerID, status string, totalMinor int64) string {
	t.Helper()
	ctx, db := context.Background(), app.App().Deps().DB
	restaurantID := "rst_" + strings.ReplaceAll(orgID, "org_", "")
	orderID := fmt.Sprintf("ord_%s_%d", restaurantID, totalMinor)
	if _, err := db.Exec(ctx, insertRestaurantSQL, restaurantID, orgID, ownerID, "Restaurant "+orgID); err != nil {
		t.Fatalf("insert restaurant: %v", err)
	}
	if _, err := db.Exec(ctx, insertOrderSQL, orderID, orgID, restaurantID, customerID, status, totalMinor); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	return orderID
}

// apiPayment is a payment as the API returns it.
type apiPayment struct {
	ID            string `json:"id"`
	OrderID       string `json:"order_id"`
	AmountMinor   int64  `json:"amount_minor"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
	ProviderRef   string `json:"provider_ref"`
	FailureReason string `json:"failure_reason"`
	Version       int64  `json:"version"`
}

// deliveryResult is what the webhook answers.
type deliveryResult struct {
	Applied   bool   `json:"applied"`
	PaymentID string `json:"payment_id"`
	Status    string `json:"status"`
}

// deliver posts body to the webhook with header, as the provider does. It
// goes through Client.Do rather than Client.Post because the signature
// covers the exact bytes.
func deliver(app *gorbitaltest.App, header http.Header, body string) *gorbitaltest.Response {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/payments", strings.NewReader(body))
	req.Header = header.Clone()
	req.Header.Set("Content-Type", "application/json")
	return app.Client().Do(req)
}

// event is a delivery body: the provider's event ID, its type, and the
// payment it is about.
func event(id, kind, paymentID string) string {
	return `{"id":"` + id + `","type":"` + kind + `","data":{"payment_id":"` + paymentID + `"}}`
}

// counts returns how many payments and how many provider events are stored.
func counts(t *testing.T, app *gorbitaltest.App) (paymentRows, eventRows int) {
	t.Helper()
	ctx, db := context.Background(), app.App().Deps().DB
	if err := db.QueryRow(ctx, `SELECT count(*) FROM order_payments`).Scan(&paymentRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM payment_events`).Scan(&eventRows); err != nil {
		t.Fatal(err)
	}
	return paymentRows, eventRows
}

// auditTrail returns the audit actions recorded for a payment, with the
// actor kind and organisation of each, oldest first.
func auditTrail(t *testing.T, app *gorbitaltest.App, paymentID string) []string {
	t.Helper()
	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action, actor_kind, coalesce(org_id, '') FROM audit_events
		 WHERE resource_type = 'payment' AND resource_id = $1 ORDER BY id`, paymentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var action, kind, orgID string
		if err := rows.Scan(&action, &kind, &orgID); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, action+" by "+kind+" in "+orgID)
	}
	return trail
}

// TestCustomerPaysForTheirOwnOrder: a customer who is a member of no
// organisation pays, gets a pending payment with the provider's reference,
// and can read it back. Paying again returns the payment they already have
// rather than charging them twice, with or without an Idempotency-Key.
func TestCustomerPaysForTheirOwnOrder(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 2350)

	res := customer.Post("/v1/orders/"+orderID+"/pay", nil)
	res.AssertStatus(t, http.StatusCreated)
	var paid apiPayment
	res.JSON(t, &paid)
	switch {
	case !strings.HasPrefix(paid.ID, "pay_"):
		t.Errorf("payment ID = %q, want a pay_ ID", paid.ID)
	case paid.Status != "pending":
		t.Errorf("status = %q, want pending: the provider hasn't answered yet", paid.Status)
	case paid.AmountMinor != 2350 || paid.Currency != "GBP":
		t.Errorf("amount = %d %s, want the order's 2350 GBP to the penny", paid.AmountMinor, paid.Currency)
	case !strings.HasPrefix(paid.ProviderRef, "pref_"):
		t.Errorf("provider_ref = %q, want the provider's reference", paid.ProviderRef)
	}

	var got apiPayment
	customer.Get("/v1/orders/"+orderID+"/payment").JSON(t, &got)
	if got != paid {
		t.Errorf("GET payment = %+v, want %+v", got, paid)
	}

	// Paying again, with no header at all: the operation is idempotent by
	// itself, because the order already has a pending payment.
	again := customer.Post("/v1/orders/"+orderID+"/pay", nil)
	again.AssertStatus(t, http.StatusCreated)
	var second apiPayment
	again.JSON(t, &second)
	if second.ID != paid.ID {
		t.Errorf("second pay = %s, want the payment already made, %s", second.ID, paid.ID)
	}
	if again.Header.Get("Idempotent-Replayed") != "" {
		t.Error("a second pay without a key was replayed from the idempotency store; it should have run")
	}
	if rows, _ := counts(t, app); rows != 1 {
		t.Errorf("%d payments after paying twice, want 1", rows)
	}

	// The customer is in no organisation, so the event's organisation is
	// the restaurant's and had to be set by the use case.
	if trail := auditTrail(t, app, paid.ID); len(trail) != 1 || trail[0] != usecase.ActionCreated+" by user in "+orgID {
		t.Errorf("audit trail = %v, want one %s by the customer in %s", trail, usecase.ActionCreated, orgID)
	}
}

// TestIdempotencyKeyReplaysTheFirstResponse: gorbital's middleware already
// honours Idempotency-Key on every POST, so a client that retries after a
// timeout gets the first response back rather than a second attempt. The
// module declares nothing for this; it only documents the header.
func TestIdempotencyKeyReplaysTheFirstResponse(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 1799)

	retrying := customer.WithHeader("Idempotency-Key", "key_ada_pays_once")
	first := retrying.Post("/v1/orders/"+orderID+"/pay", nil)
	first.AssertStatus(t, http.StatusCreated)
	if replayed := first.Header.Get("Idempotent-Replayed"); replayed != "" {
		t.Errorf("first response Idempotent-Replayed = %q, want it absent", replayed)
	}

	retry := retrying.Post("/v1/orders/"+orderID+"/pay", nil)
	retry.AssertStatus(t, http.StatusCreated)
	if replayed := retry.Header.Get("Idempotent-Replayed"); replayed != "true" {
		t.Errorf("retry Idempotent-Replayed = %q, want true", replayed)
	}
	if string(retry.Body) != string(first.Body) {
		t.Errorf("retry body = %s, want the stored first response %s", retry.Body, first.Body)
	}
	if rows, _ := counts(t, app); rows != 1 {
		t.Errorf("%d payments after a retried request, want 1", rows)
	}
}

// TestSomebodyElsesOrderIsNotFound: the route's guard only says that the
// caller is signed in. What makes an order theirs is a column, so the check
// is in the use case — and an order belonging to somebody else answers 404,
// the same as one that doesn't exist, so order IDs can't be probed.
func TestSomebodyElsesOrderIsNotFound(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	ada, adaID, _ := signUp(t, app, "ada@example.com")
	bob, _, _ := signUp(t, app, "bob@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, adaID, "placed", 2350)

	bob.Post("/v1/orders/"+orderID+"/pay", nil).AssertProblem(t, http.StatusNotFound, "order_not_found")
	bob.Post("/v1/orders/ord_missing/pay", nil).AssertProblem(t, http.StatusNotFound, "order_not_found")

	ada.Post("/v1/orders/"+orderID+"/pay", nil).AssertStatus(t, http.StatusCreated)
	bob.Get("/v1/orders/"+orderID+"/payment").AssertProblem(t, http.StatusNotFound, "payment_not_found")

	// Nobody signed in at all doesn't get as far as the use case.
	app.Client().Post("/v1/orders/"+orderID+"/pay", nil).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	if rows, _ := counts(t, app); rows != 1 {
		t.Errorf("%d payments, want only Ada's", rows)
	}
}

// TestAuthorisedEventMovesThePayment: the provider confirms by webhook. The
// payment moves to authorised, the event is recorded, and an audit event is
// written for a change with nobody signed in behind it. A redelivery of the
// same event ID changes nothing at all.
func TestAuthorisedEventMovesThePayment(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 2350)

	var paid apiPayment
	customer.Post("/v1/orders/"+orderID+"/pay", nil).JSON(t, &paid)

	body := event("evt_1", "payment.authorised", paid.ID)
	res := deliver(app, sign("evt_1", time.Now(), body), body)
	res.AssertStatus(t, http.StatusOK)
	var applied deliveryResult
	res.JSON(t, &applied)
	if !applied.Applied || applied.Status != "authorised" || applied.PaymentID != paid.ID {
		t.Fatalf("delivery = %+v, want the payment authorised", applied)
	}

	var authorised apiPayment
	customer.Get("/v1/orders/"+orderID+"/payment").JSON(t, &authorised)
	if authorised.Status != "authorised" || authorised.Version != paid.Version+1 {
		t.Errorf("payment = %+v, want authorised at version %d", authorised, paid.Version+1)
	}

	// The retry the provider sends when it isn't sure the first arrived: a
	// fresh timestamp and signature, the same event ID.
	retry := deliver(app, sign("evt_1", time.Now().Add(time.Minute), body), body)
	retry.AssertStatus(t, http.StatusOK)
	var replayed deliveryResult
	retry.JSON(t, &replayed)
	if replayed.Applied || replayed.Status != "authorised" {
		t.Errorf("redelivery = %+v, want applied false and the payment untouched", replayed)
	}
	var afterRetry apiPayment
	customer.Get("/v1/orders/"+orderID+"/payment").JSON(t, &afterRetry)
	if afterRetry != authorised {
		t.Errorf("payment after the redelivery = %+v, want it unchanged at %+v", afterRetry, authorised)
	}
	if rows, events := counts(t, app); rows != 1 || events != 1 {
		t.Errorf("after a delivery and its retry: %d payments, %d events; want 1 and 1", rows, events)
	}

	// The webhook has no signed-in actor, so the use case names the service
	// and the restaurant's organisation itself.
	want := []string{
		usecase.ActionCreated + " by user in " + orgID,
		usecase.ActionAuthorised + " by service in " + orgID,
	}
	if trail := auditTrail(t, app, paid.ID); len(trail) != 2 || trail[0] != want[0] || trail[1] != want[1] {
		t.Errorf("audit trail = %v, want %v", trail, want)
	}
}

// TestUnauthenticDeliveriesChangeNothing: a delivery signed with another
// secret, one changed after it was signed, and one with no headers at all
// get the same 401 from guard.Webhook, before any handler of this module
// runs — so none of them moves a payment or records an event.
func TestUnauthenticDeliveriesChangeNothing(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 2350)

	var paid apiPayment
	customer.Post("/v1/orders/"+orderID+"/pay", nil).JSON(t, &paid)
	body := event("evt_1", "payment.authorised", paid.ID)

	forged := sign("evt_1", time.Now(), body)
	wrongKey := hmac.New(sha256.New, []byte("another-signing-secret-32-bytes."))
	fmt.Fprintf(wrongKey, "evt_1.%d.%s", time.Now().Unix(), body)
	forged.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(wrongKey.Sum(nil)))

	for name, delivery := range map[string]struct {
		header http.Header
		body   string
	}{
		"wrong secret":   {forged, body},
		"changed body":   {sign("evt_1", time.Now(), body), event("evt_1", "payment.captured", paid.ID)},
		"no signature":   {http.Header{}, body},
		"outside window": {sign("evt_1", time.Now().Add(-10*time.Minute), body), body},
	} {
		t.Run(name, func(t *testing.T) {
			deliver(app, delivery.header, delivery.body).
				AssertProblem(t, http.StatusUnauthorized, "invalid_webhook_signature")
		})
	}

	var after apiPayment
	customer.Get("/v1/orders/"+orderID+"/payment").JSON(t, &after)
	if after.Status != "pending" || after.Version != paid.Version {
		t.Errorf("payment = %+v, want it still pending at version %d", after, paid.Version)
	}
	if _, events := counts(t, app); events != 0 {
		t.Errorf("%d events recorded from four refused deliveries, want none", events)
	}
}

// TestUnknownEventTypeIsAcceptedAndIgnored: a signature proves who sent the
// delivery, not that this app knows what it means. An event type nobody
// here acts on is accepted, so the provider stops retrying it, and changes
// nothing.
func TestUnknownEventTypeIsAcceptedAndIgnored(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 2350)

	var paid apiPayment
	customer.Post("/v1/orders/"+orderID+"/pay", nil).JSON(t, &paid)

	body := event("evt_7", "payment.disputed", paid.ID)
	res := deliver(app, sign("evt_7", time.Now(), body), body)
	res.AssertStatus(t, http.StatusOK)
	var ignored deliveryResult
	res.JSON(t, &ignored)
	if ignored.Applied || ignored.PaymentID != "" {
		t.Errorf("unknown event type = %+v, want accepted and ignored", ignored)
	}

	var after apiPayment
	customer.Get("/v1/orders/"+orderID+"/payment").JSON(t, &after)
	if after != paid {
		t.Errorf("payment = %+v, want it untouched at %+v", after, paid)
	}
	if _, events := counts(t, app); events != 0 {
		t.Errorf("%d events recorded for an event type nobody acts on, want none", events)
	}
}

// TestPlatformStaffRefundOnce: a platform admin gives the money back, and a
// second refund of the same payment is refused by the same rule that allows
// the first.
func TestPlatformStaffRefundOnce(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")
	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 2350)

	var paid apiPayment
	customer.Post("/v1/orders/"+orderID+"/pay", nil).JSON(t, &paid)
	body := event("evt_1", "payment.authorised", paid.ID)
	deliver(app, sign("evt_1", time.Now(), body), body).AssertStatus(t, http.StatusOK)

	refundPath := "/v1/platform/payments/" + paid.ID + "/refund"
	reason := map[string]any{"reason": "The restaurant never cooked it"}

	// A customer has the pay permission, not the platform's refund one.
	customer.Post(refundPath, reason).AssertProblem(t, http.StatusForbidden, "forbidden")

	staff := app.As(gorbitaltest.User("usr_platform", usecase.PermRefund))
	res := staff.Post(refundPath, reason)
	res.AssertStatus(t, http.StatusOK)
	var refunded apiPayment
	res.JSON(t, &refunded)
	if refunded.Status != "refunded" {
		t.Fatalf("refund = %+v, want the payment refunded", refunded)
	}

	// Refunded is final: there is nothing left to give back.
	staff.Post(refundPath, reason).AssertProblem(t, http.StatusConflict, "payment_not_refundable")

	// The reason an operator wrote is what the audit event is for.
	var metadata string
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT coalesce(metadata::text, '') FROM audit_events
		 WHERE resource_id = $1 AND action = $2`, paid.ID, usecase.ActionRefunded).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata, "never cooked it") {
		t.Errorf("refund audit metadata = %s, want the operator's reason", metadata)
	}
	want := usecase.ActionRefunded + " by user in " + orgID
	trail := auditTrail(t, app, paid.ID)
	if len(trail) != 3 || trail[2] != want {
		t.Errorf("audit trail = %v, want it to end with %q", trail, want)
	}
}

// TestAnOrderThatIsNotWaitingToBePaid: the order must still be placed, and
// a payment that has moved past pending is not paid a second time.
func TestAnOrderThatIsNotWaitingToBePaid(t *testing.T) {
	app := newApp(t)
	_, restaurateurID, orgID := signUp(t, app, "chef@example.com")
	customer, customerID, _ := signUp(t, app, "ada@example.com")

	cancelled := seedOrder(t, app, orgID, restaurateurID, customerID, "cancelled", 900)
	customer.Post("/v1/orders/"+cancelled+"/pay", nil).AssertProblem(t, http.StatusConflict, "order_not_payable")

	orderID := seedOrder(t, app, orgID, restaurateurID, customerID, "placed", 2350)
	var paid apiPayment
	customer.Post("/v1/orders/"+orderID+"/pay", nil).JSON(t, &paid)
	body := event("evt_1", "payment.authorised", paid.ID)
	deliver(app, sign("evt_1", time.Now(), body), body).AssertStatus(t, http.StatusOK)

	// The money has moved on; a second attempt isn't a retry of the first.
	customer.Post("/v1/orders/"+orderID+"/pay", nil).AssertProblem(t, http.StatusConflict, "order_not_payable")
}
