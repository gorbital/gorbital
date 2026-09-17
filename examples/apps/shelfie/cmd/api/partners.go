package main

import (
	"os"
	"strings"
)

// docs:start partner-secrets

// partnerName is the bookshop whose purchases Shelfie records. A second
// partner gets its own secret, its own route and its own name here.
const partnerName = "pagebound"

// partnerSecrets are the signing secrets of the partner's webhooks, from
// PARTNER_WEBHOOK_SECRET: one, or two separated by a comma while the shop
// rotates it (add the new one, let the shop switch, then remove the old).
//
// Empty is not an error: the module then refuses every delivery, because an
// app that isn't configured for the partner must not accept what it sends.
// A secret the library won't take stops the app at startup, with the other
// configuration errors.
func partnerSecrets() []string {
	var out []string
	for _, s := range strings.Split(os.Getenv("PARTNER_WEBHOOK_SECRET"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// docs:end partner-secrets
