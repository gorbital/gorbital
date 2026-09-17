package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/jwt"
)

// docs:start authenticator

// identityProvider is the app's authenticator: it verifies the access
// tokens the identity provider issued to the mobile app.
//
// gorbital.WithAuth takes anything with a Middleware method, which
// *jwt.Authenticator has; but jwt.New takes a context and fetches the
// provider's keys, and main has neither a context nor a place to report an
// error. So this type is what main passes, and gorbital's two optional
// hooks do the work in the right order:
//
//   - CheckConfig reads IDP_* before the app connects to anything, so a
//     missing or malformed setting exits with status 2, with the other
//     configuration errors;
//   - Setup runs once the app is being built, with a context, and is where
//     jwt.New fetches the keys, so an unreachable provider or a mistyped
//     JWKS URL stops the deployment instead of failing requests later.
//
// Neither runs while the OpenAPI document is exported, which needs no
// configuration and reaches nothing.
type identityProvider struct {
	src  config.Source
	cfg  jwt.Config
	auth *jwt.Authenticator
}

func newIdentityProvider(src config.Source) *identityProvider {
	return &identityProvider{src: src}
}

// CheckConfig reads the IDP_* variables. gorbital reports the error as a
// configuration error, like an invalid DATABASE_URL.
func (p *identityProvider) CheckConfig(gorbital.Config) error {
	cfg, err := idpConfig(p.src)
	p.cfg = cfg
	return err
}

// Setup fetches the provider's keys, within ctx.
func (p *identityProvider) Setup(ctx context.Context, _ gorbital.AuthSetup) error {
	auth, err := jwt.New(ctx, p.cfg)
	if err != nil {
		return fmt.Errorf("identity provider %s: %w", p.cfg.Issuer, err)
	}
	p.auth = auth
	return nil
}

// Middleware verifies the bearer token of every request and puts the caller
// in the context as an actor. gorbital calls it once, after Setup, when it
// builds the middleware stack.
func (p *identityProvider) Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return p.auth.Middleware(logger)
}

// docs:end authenticator

// docs:start idp-config

// idpConfig builds the authenticator's configuration from the environment,
// reporting every problem at once, as gorbital.LoadConfig does. The
// defaults are the library's: RS256, ES256 and EdDSA, 30 seconds of clock
// skew, and the "permissions" claim.
func idpConfig(src config.Source) (jwt.Config, error) {
	get := func(key string, problems *[]error) string {
		v, err := src.Get(key)
		if err != nil {
			*problems = append(*problems, err)
		}
		return strings.TrimSpace(v)
	}
	var problems []error
	cfg := jwt.Config{
		// The provider's issuer, exactly as its tokens spell it, with the
		// trailing slash when it has one.
		Issuer: get("IDP_ISSUER", &problems),
		// What identifies this API to the provider. A token for another of
		// the provider's APIs is refused.
		Audiences: split(get("IDP_AUDIENCE", &problems)),
		// Cognito access tokens carry the audience in "client_id".
		AudienceClaim: get("IDP_AUDIENCE_CLAIM", &problems),
		// Where the provider publishes its public keys.
		JWKSURL: get("IDP_JWKS_URL", &problems),
		// The signature algorithms the provider actually uses.
		Algorithms: split(get("IDP_ALGORITHMS", &problems)),
		// The claim the caller's permissions are read from: the app's own
		// permission names, trips.trip.read and trips.trip.write.
		PermissionsClaim: get("IDP_PERMISSIONS_CLAIM", &problems),
	}
	if skew := get("IDP_CLOCK_SKEW", &problems); skew != "" {
		d, err := time.ParseDuration(skew)
		if err != nil {
			problems = append(problems, fmt.Errorf("IDP_CLOCK_SKEW: %q is not a duration such as 30s", skew))
		}
		cfg.ClockSkew = d
	}
	for _, required := range [][2]string{
		{"IDP_ISSUER", cfg.Issuer},
		{"IDP_AUDIENCE", strings.Join(cfg.Audiences, "")},
		{"IDP_JWKS_URL", cfg.JWKSURL},
	} {
		if required[1] == "" {
			problems = append(problems, fmt.Errorf("%s: required; see .env.example", required[0]))
		}
	}
	return cfg, errors.Join(problems...)
}

// split returns the non-empty, trimmed parts of a comma-separated list.
func split(v string) []string {
	var list []string
	for part := range strings.SplitSeq(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			list = append(list, part)
		}
	}
	return list
}

// docs:end idp-config
