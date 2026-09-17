package partners_test

import (
	"bytes"
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

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/partners"
	"example.com/shelfie/internal/modules/partners/usecase"
)

// These tests drive the partner webhook through the app's real middleware
// stack and guards, on a new database per test (gorbitaltest).

// testSecret is a Standard Webhooks signing secret: whsec_ and 24 bytes of
// base64. It is a fixed test value, never a real secret.
const testSecret = "whsec_" + "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcY" // gitleaks:allow (test value)

const partner = "pagebound"

// newApp builds the app with the partners module, configured for the
// partner, as main.go wires it.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithModules(partners.Module(partner, []string{testSecret})),
		gorbital.WithMigrations(migrations.FS),
	)
}

const webhookPath = "/v1/webhooks/partners/purchases"

// docs:start sign

// sign returns the Standard Webhooks headers a partner sends with body:
// the delivery's ID, the time it was signed, and an HMAC-SHA256 of
// "<id>.<timestamp>.<body>" under the shared secret.
func sign(secret, id, body string, at time.Time) http.Header {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	return http.Header{
		"Webhook-Id":        {id},
		"Webhook-Timestamp": {ts},
		"Webhook-Signature": {"v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))},
		"Content-Type":      {"application/json"},
	}
}

// docs:end sign

// deliver sends body with the headers h, exactly as a partner's client
// would: the bytes that were signed are the bytes the app verifies.
func deliver(t *testing.T, app *gorbitaltest.App, body string, h http.Header) *gorbitaltest.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader([]byte(body)))
	req.Header = h
	return app.Client().Do(req)
}

// event is a purchase a partner reports.
func event(id, reader, isbn string, at time.Time) string {
	return fmt.Sprintf(`{"event_id":%q,"user_id":%q,"isbn":%q,"title":"The Odyssey","purchased_at":%q}`,
		id, reader, isbn, at.UTC().Format(time.RFC3339))
}

type apiPurchase struct {
	ID          string `json:"id"`
	Partner     string `json:"partner"`
	ISBN        string `json:"isbn"`
	Title       string `json:"title"`
	PurchasedAt string `json:"purchased_at"`
}

// docs:start replay-test

// TestAPurchaseIsRecordedOnce checks the two halves of receiving a webhook:
// a signed delivery is recorded, and the same delivery again changes
// nothing. Senders retry, and a captured request can be replayed inside the
// signature's five-minute window, so "verified" is not "new".
func TestAPurchaseIsRecordedOnce(t *testing.T) {
	app := newApp(t)
	now := time.Now()
	body := event("evt_1", "usr_ada", "9780140449136", now.Add(-time.Hour))
	headers := sign(testSecret, "evt_1", body, now)

	res := deliver(t, app, body, headers)
	res.AssertStatus(t, http.StatusOK)
	var first apiPurchase
	res.JSON(t, &first)
	if !strings.HasPrefix(first.ID, "prc_") || first.Partner != partner || first.Title != "The Odyssey" {
		t.Errorf("first delivery = %+v, want a prc_ ID and the partner's purchase", first)
	}

	again := deliver(t, app, body, headers)
	again.AssertStatus(t, http.StatusOK)
	var second apiPurchase
	again.JSON(t, &second)
	if second.ID != first.ID {
		t.Errorf("replay recorded a second purchase: %s then %s", first.ID, second.ID)
	}

	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead))
	var list struct {
		Items []apiPurchase `json:"items"`
	}
	ada.Get("/v1/purchases").JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0].ID != first.ID {
		t.Errorf("Ada's purchases = %+v, want the one purchase", list.Items)
	}
}

// docs:end replay-test

// docs:start refused-test

// TestUnsignedDeliveriesAreRefused checks that guard.Webhook answers 401
// before the body is parsed, whatever is wrong with the signature, and that
// nothing is written.
func TestUnsignedDeliveriesAreRefused(t *testing.T) {
	app := newApp(t)
	now := time.Now()
	body := event("evt_2", "usr_ada", "9780140449136", now.Add(-time.Hour))

	other := "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 24))
	for name, headers := range map[string]http.Header{
		"no signature":     {"Content-Type": {"application/json"}},
		"another secret":   sign(other, "evt_2", body, now),
		"changed body":     sign(testSecret, "evt_2", event("evt_2", "usr_mallory", "9780140449136", now.Add(-time.Hour)), now),
		"signed yesterday": sign(testSecret, "evt_2", body, now.Add(-24*time.Hour)),
		"from the future":  sign(testSecret, "evt_2", body, now.Add(24*time.Hour)),
	} {
		t.Run(name, func(t *testing.T) {
			deliver(t, app, body, headers).AssertProblem(t, http.StatusUnauthorized, "invalid_webhook_signature")
		})
	}

	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead))
	var list struct {
		Items []apiPurchase `json:"items"`
	}
	ada.Get("/v1/purchases").JSON(t, &list)
	if len(list.Items) != 0 {
		t.Errorf("refused deliveries wrote %d purchases", len(list.Items))
	}
}

// docs:end refused-test

// TestASignedDeliveryStillHasToBeValid checks that a verified signature
// doesn't exempt a delivery from the module's rules: the signature says who
// sent it, not that what they sent makes sense.
func TestASignedDeliveryStillHasToBeValid(t *testing.T) {
	app := newApp(t)
	now := time.Now()
	for name, tc := range map[string]struct{ id, body, code string }{
		"not an ISBN":      {"dl_1", event("evt_3", "usr_ada", "123", now.Add(-time.Hour)), "invalid_isbn"},
		"no reader":        {"dl_2", event("evt_4", "", "9780140449136", now.Add(-time.Hour)), "invalid_reader"},
		"bought tomorrow":  {"dl_3", event("evt_5", "usr_ada", "9780140449136", now.Add(24*time.Hour)), "invalid_purchased_at"},
		"no delivery ID":   {"dl_4", event("", "usr_ada", "9780140449136", now.Add(-time.Hour)), "invalid_event_id"},
		"body isn't JSON":  {"dl_5", `{"event_id":`, "bad_request"},
		"body is an array": {"dl_6", `[]`, "validation_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			res := deliver(t, app, tc.body, sign(testSecret, tc.id, tc.body, now))
			if res.Status == http.StatusOK {
				t.Fatalf("%s was accepted: %s", name, res.Body)
			}
			var problem struct {
				Code string `json:"code"`
			}
			res.JSON(t, &problem)
			if problem.Code != tc.code {
				t.Errorf("code = %q, want %q (status %d)", problem.Code, tc.code, res.Status)
			}
		})
	}
}

// TestPurchasesAreTheReadersOwn checks that one reader never sees another's
// purchases, and that the list needs a signed-in reader.
func TestPurchasesAreTheReadersOwn(t *testing.T) {
	app := newApp(t)
	now := time.Now()
	for i, reader := range []string{"usr_ada", "usr_bob"} {
		id := fmt.Sprintf("evt_%d", i)
		body := event(id, reader, "978014044913"+strconv.Itoa(i), now.Add(-time.Hour))
		deliver(t, app, body, sign(testSecret, id, body, now)).AssertStatus(t, http.StatusOK)
	}

	var list struct {
		Items []apiPurchase `json:"items"`
	}
	app.As(gorbitaltest.User("usr_ada", usecase.PermRead)).Get("/v1/purchases").JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0].ISBN != "9780140449130" {
		t.Errorf("Ada's purchases = %+v, want only her own", list.Items)
	}

	app.Client().Get("/v1/purchases").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	app.As(gorbitaltest.User("usr_bob")).Get("/v1/purchases").AssertProblem(t, http.StatusForbidden, "forbidden")
}

// TestWithoutASecretEveryDeliveryIsRefused checks that an app with no
// PARTNER_WEBHOOK_SECRET refuses what the partner sends instead of
// trusting it, and still serves the route.
func TestWithoutASecretEveryDeliveryIsRefused(t *testing.T) {
	app := gorbitaltest.New(t,
		gorbital.WithModules(partners.Module(partner, nil)),
		gorbital.WithMigrations(migrations.FS),
	)
	now := time.Now()
	body := event("evt_6", "usr_ada", "9780140449136", now.Add(-time.Hour))
	deliver(t, app, body, sign(testSecret, "evt_6", body, now)).
		AssertProblem(t, http.StatusUnauthorized, "invalid_webhook_signature")
}
