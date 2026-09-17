package trips_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/modules/jwt"

	"example.com/mobile-backend/db/migrations"
	"example.com/mobile-backend/internal/modules/trips"
	"example.com/mobile-backend/internal/modules/trips/usecase"
)

// docs:start test-app

// newApp builds the mobile backend on a new database for the test, with the
// trips module and an authenticator that trusts the local provider p. It is
// the wiring of main.go, with p's address instead of IDP_JWKS_URL's.
func newApp(t *testing.T, p *idp) *gorbitaltest.App {
	t.Helper()
	auth, err := jwt.New(t.Context(), jwt.Config{
		Issuer:    testIssuer,
		Audiences: []string{testAudience},
		JWKSURL:   p.url(),
	}, jwt.WithClock(p.clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	return gorbitaltest.New(t,
		gorbital.WithName("mobile-backend"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(trips.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// as returns a client sending token in the Authorization header, as the
// mobile app does with the token the provider gave it.
func as(app *gorbitaltest.App, token string) *gorbitaltest.Client {
	return app.Client().WithHeader("Authorization", "Bearer "+token)
}

// docs:end test-app

type trip struct {
	ID          string    `json:"id"`
	Destination string    `json:"destination"`
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"created_at"`
}

type tripList struct {
	Items []trip `json:"items"`
}

// docs:start valid-token

// TestATokenReachesOnlyItsOwnTrips: a token the provider signed reaches the
// routes, and what it writes belongs to the token's subject.
func TestATokenReachesOnlyItsOwnTrips(t *testing.T) {
	p := newIDP(t)
	app := newApp(t, p)
	ada := as(app, p.token("auth0|ada", usecase.PermRead, usecase.PermWrite))

	res := ada.Post("/v1/trips", map[string]string{"destination": "Lisbon", "notes": "Three nights in Alfama."})
	res.AssertStatus(t, http.StatusCreated)
	var added trip
	res.JSON(t, &added)

	// The request said nothing about an owner: the row belongs to the
	// token's "sub" claim, which is the actor's ID.
	var owner string
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT owner_id FROM trips WHERE id = $1`, added.ID).Scan(&owner); err != nil || owner != "auth0|ada" {
		t.Fatalf("owner_id = %q, %v; want auth0|ada", owner, err)
	}

	var list tripList
	ada.Get("/v1/trips").JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0].ID != added.ID {
		t.Errorf("GET /v1/trips = %+v, want the trip just added", list.Items)
	}
	var got trip
	ada.Get("/v1/trips/"+added.ID).JSON(t, &got)
	if got.Destination != "Lisbon" {
		t.Errorf("GET /v1/trips/{id} = %+v, want Lisbon", got)
	}
	ada.Delete("/v1/trips/"+added.ID).AssertStatus(t, http.StatusNoContent)
	ada.Get("/v1/trips/"+added.ID).AssertProblem(t, http.StatusNotFound, "trip_not_found")

	// The audit log records the provider's subject as the actor.
	var actorID string
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT actor_id FROM audit_events WHERE action = $1`, usecase.ActionAdded).Scan(&actorID); err != nil || actorID != "auth0|ada" {
		t.Errorf("audit actor = %q, %v; want auth0|ada", actorID, err)
	}
}

// docs:end valid-token

// docs:start refused-tokens

// TestTokensTheAPIRefuses: everything that isn't a valid, current token
// from this provider for this API.
func TestTokensTheAPIRefuses(t *testing.T) {
	p := newIDP(t)
	app := newApp(t, p)
	permissions := []string{usecase.PermRead}
	key, kid := p.signingKey()

	elsewhere := p.claims("auth0|ada", permissions)
	elsewhere["aud"] = "https://another-api.example.test"
	expired := p.claims("auth0|ada", permissions)
	expired["exp"] = p.clock.Now().Add(-time.Hour).Unix()
	anotherIssuer := p.claims("auth0|ada", permissions)
	anotherIssuer["iss"] = "https://impostor.example.test/"

	for _, tc := range []struct {
		name          string
		authorization string
		status        int
		code          string
	}{
		{"no token", "", http.StatusUnauthorized, "unauthenticated"},
		{"an expired token", "Bearer " + p.sign(key, kid, expired), http.StatusUnauthorized, "invalid_token"},
		{"a token for another API", "Bearer " + p.sign(key, kid, elsewhere), http.StatusUnauthorized, "invalid_token"},
		{"a token from another issuer", "Bearer " + p.sign(key, kid, anotherIssuer), http.StatusUnauthorized, "invalid_token"},
		// Signed with a key the provider never published, under the key ID
		// of one it did: the signature doesn't check out.
		{"a forged token", "Bearer " + p.sign(testRSAKeys()[2], kid, p.claims("auth0|ada", permissions)), http.StatusUnauthorized, "invalid_token"},
		// Not shaped like a JWT: the request continues without an actor,
		// and the route's guard answers, as it does for no token at all.
		{"an API key", "Bearer sk_live_not_a_jwt", http.StatusUnauthorized, "unauthenticated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := app.Client()
			if tc.authorization != "" {
				client = client.WithHeader("Authorization", tc.authorization)
			}
			res := client.Get("/v1/trips")
			res.AssertProblem(t, tc.status, tc.code)
			if tc.code == "invalid_token" && res.Header.Get("WWW-Authenticate") != `Bearer error="invalid_token"` {
				t.Errorf("WWW-Authenticate = %q, want Bearer error=\"invalid_token\"", res.Header.Get("WWW-Authenticate"))
			}
		})
	}
}

// docs:end refused-tokens

// docs:start other-travellers

// TestAnotherTravellersTripIsNotFound: two travellers of the same provider
// never see each other's trips, and an ID tells one nothing about the
// other.
func TestAnotherTravellersTripIsNotFound(t *testing.T) {
	p := newIDP(t)
	app := newApp(t, p)
	both := []string{usecase.PermRead, usecase.PermWrite}
	ada := as(app, p.token("auth0|ada", both...))
	bo := as(app, p.token("auth0|bo", both...))

	var adas trip
	ada.Post("/v1/trips", map[string]string{"destination": "Lisbon"}).JSON(t, &adas)

	// Bo holds every permission the routes ask for, and still can't reach
	// Ada's trip: the use cases scope each operation to the caller.
	bo.Get("/v1/trips/"+adas.ID).AssertProblem(t, http.StatusNotFound, "trip_not_found")
	bo.Delete("/v1/trips/"+adas.ID).AssertProblem(t, http.StatusNotFound, "trip_not_found")

	var list tripList
	bo.Get("/v1/trips").JSON(t, &list)
	if len(list.Items) != 0 {
		t.Errorf("Bo's trips = %+v, want none", list.Items)
	}
	// Ada's trip is still there.
	ada.Get("/v1/trips/"+adas.ID).AssertStatus(t, http.StatusOK)
}

// docs:end other-travellers

// docs:start permissions-from-claims

// TestPermissionsComeFromTheToken: the guards hold the permissions the
// provider put in the token, so a token without one is refused before the
// use case runs.
func TestPermissionsComeFromTheToken(t *testing.T) {
	p := newIDP(t)
	app := newApp(t, p)
	reader := as(app, p.token("auth0|ada", usecase.PermRead))
	none := as(app, p.token("auth0|bo"))

	reader.Post("/v1/trips", map[string]string{"destination": "Lisbon"}).
		AssertProblem(t, http.StatusForbidden, "forbidden")
	reader.Delete("/v1/trips/trp_whatever").AssertProblem(t, http.StatusForbidden, "forbidden")
	reader.Get("/v1/trips").AssertStatus(t, http.StatusOK)

	none.Get("/v1/trips").AssertProblem(t, http.StatusForbidden, "forbidden")
}

// docs:end permissions-from-claims

// docs:start rotation-and-outage

// TestKeyRotationAndOutage: the app follows the provider's key rotation
// without a restart, keeps working from its cache while the provider is
// unreachable, and says so when it needs a key it hasn't got.
func TestKeyRotationAndOutage(t *testing.T) {
	p := newIDP(t)
	app := newApp(t, p) // fetched the keys once, at start
	read := []string{usecase.PermRead}

	p.rotate(testRSAKeys()[1], "key-2")
	// The authenticator fetches the key set again for a key ID it doesn't
	// know, at most once every 30 seconds.
	p.clock.Add(time.Minute)
	rotated := p.token("auth0|ada", read...)
	as(app, rotated).Get("/v1/trips").AssertStatus(t, http.StatusOK)

	// The provider goes down. Tokens signed with a cached key are still
	// verified: nothing is asked of the provider.
	p.setDown(true)
	p.clock.Add(time.Minute)
	as(app, rotated).Get("/v1/trips").AssertStatus(t, http.StatusOK)

	// A token naming a key the cache hasn't got can't be verified at all,
	// which is a 503 the client should retry, not a 401 it should react to
	// by signing the traveller out.
	unknown := p.sign(testRSAKeys()[2], "key-3", p.claims("auth0|ada", read))
	as(app, unknown).Get("/v1/trips").AssertProblem(t, http.StatusServiceUnavailable, "auth_unavailable")
}

// docs:end rotation-and-outage

// TestTripRules: the body the API refuses, whoever sends it.
func TestTripRules(t *testing.T) {
	p := newIDP(t)
	app := newApp(t, p)
	ada := as(app, p.token("auth0|ada", usecase.PermRead, usecase.PermWrite))

	ada.Post("/v1/trips", map[string]string{"destination": "  "}).
		AssertProblem(t, http.StatusUnprocessableEntity, "destination_required")
	ada.Get("/v1/trips/trp_missing").AssertProblem(t, http.StatusNotFound, "trip_not_found")
}
