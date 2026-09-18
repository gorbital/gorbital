package main

import (
	"os"

	"gorbital.dev/gorbital/authhttp"

	"example.com/plateful/internal/modules/notifications"
	"example.com/plateful/internal/modules/orders"
)

// docs:start sign-in-options

// signInOptions are how Plateful's sign-in differs from the library's
// defaults.
//
// POST /v1/auth/register takes a display name and a delivery address beside
// the email address and the password, and saves them in the transaction that
// creates the account (internal/modules/orders/hooks.go). A customer who
// registered has somewhere for their first order to go; one who signed in
// with Google for the first time never filled this form, so nothing assumes
// the profile exists.
func signInOptions() []authhttp.Option {
	return []authhttp.Option{
		authhttp.RegisterFields(orders.SaveRegistration),
		// Customers pay for food here; fourteen characters is the least a
		// password should be.
		authhttp.MinPasswordLength(14),
	}
}

// docs:end sign-in-options

// docs:start webhook-targets

// webhookTargets says which endpoint URLs the notifications module will post
// to. Production takes public https addresses only. Development also allows
// an address on this machine, so orb dev and the tests can catch the
// deliveries — which is a decision about the deployment, not about a
// restaurant, so it is here in main.go and deliberately not a runtime
// setting an operator could turn off from /ops.
func webhookTargets() notifications.Option {
	return notifications.AllowPrivateTargets(os.Getenv("APP_ENV") != "production")
}

// docs:end webhook-targets
