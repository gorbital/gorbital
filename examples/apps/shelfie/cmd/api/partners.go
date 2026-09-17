package main

import (
	"strings"

	"gorbital.dev/config"
)

// docs:start partner-secrets

// partnerName is the bookshop whose purchases Shelfie records. A second
// partner gets its own secret, its own route and its own name here.
const partnerName = "pagebound"

// partnerSecrets reads the signing secrets of the partner's webhooks from
// PARTNER_WEBHOOK_SECRET, or from the file PARTNER_WEBHOOK_SECRET_FILE
// names, as every other secret of the app is read: one secret, or two
// separated by a comma while the shop rotates it (add the new one, let the
// shop switch, then remove the old).
//
// None is not an error: the module then refuses every delivery, because an
// app that isn't configured for the partner must not accept what it sends.
func partnerSecrets() ([]string, error) {
	secret, err := config.OS.Secret("PARTNER_WEBHOOK_SECRET")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range strings.Split(secret.Reveal(), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// docs:end partner-secrets
