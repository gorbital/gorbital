package gorbitaltest_test

import (
	"fmt"
	"net/http"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/orgshttp"
	authlib "gorbital.dev/modules/auth"
)

func ExampleApp_SignUp() {
	test(func(t *testing.T) {
		// Real accounts, for features that store who the user is, such as
		// organisations: sign-in and orgshttp are the app's.
		auth := authhttp.New()
		app := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": authlib.NewKeyringKey("test")},
			gorbital.WithAuth(auth), gorbital.WithModules(orgshttp.Module(auth)))
		ada, adaID := app.SignUp(t, "ada@example.com")
		res := ada.Get("/v1/orgs")
		res.AssertStatus(t, http.StatusOK)
		var orgs struct {
			Items []struct {
				Personal bool `json:"personal"`
			} `json:"items"`
		}
		res.JSON(t, &orgs)
		if adaID == "" || len(orgs.Items) != 1 || !orgs.Items[0].Personal {
			t.Errorf("ada %q has organisations %+v, want her personal workspace", adaID, orgs.Items)
		}
	})
}

func ExampleSignUpPassword() {
	fmt.Println(len(gorbitaltest.SignUpPassword) >= 12)
	// Output: true
}
