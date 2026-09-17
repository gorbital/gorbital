package main

import (
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
)

// env returns a config source reading vars, as config.OS reads the process
// environment.
func env(vars map[string]string) config.Source {
	return config.Source{Getenv: func(key string) string { return vars[key] }}
}

// TestIDPConfig: the three required variables become a jwt.Config, and the
// optional ones are left to the library's defaults.
func TestIDPConfig(t *testing.T) {
	cfg, err := idpConfig(env(map[string]string{
		"IDP_ISSUER":     "https://idp.example.test/",
		"IDP_AUDIENCE":   "https://api.example.test, https://legacy.example.test",
		"IDP_JWKS_URL":   "https://idp.example.test/.well-known/jwks.json",
		"IDP_ALGORITHMS": "RS256",
		"IDP_CLOCK_SKEW": "1m",
	}))
	if err != nil {
		t.Fatalf("idpConfig = %v", err)
	}
	if len(cfg.Audiences) != 2 || cfg.Audiences[1] != "https://legacy.example.test" {
		t.Errorf("Audiences = %q, want both, trimmed", cfg.Audiences)
	}
	if cfg.ClockSkew != time.Minute || len(cfg.Algorithms) != 1 {
		t.Errorf("ClockSkew = %s, Algorithms = %q", cfg.ClockSkew, cfg.Algorithms)
	}
	// Unset: the library's defaults, not this app's.
	if cfg.AudienceClaim != "" || cfg.PermissionsClaim != "" {
		t.Errorf("AudienceClaim = %q, PermissionsClaim = %q; want both empty", cfg.AudienceClaim, cfg.PermissionsClaim)
	}
}

// TestIDPConfigReportsEveryProblem: a deployment that forgot the provider
// learns everything it is missing at once, before anything connects.
func TestIDPConfigReportsEveryProblem(t *testing.T) {
	_, err := idpConfig(env(map[string]string{"IDP_CLOCK_SKEW": "half an hour"}))
	if err == nil {
		t.Fatal("idpConfig = nil, want the missing variables")
	}
	for _, want := range []string{"IDP_ISSUER", "IDP_AUDIENCE", "IDP_JWKS_URL", "IDP_CLOCK_SKEW"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't name %s", err, want)
		}
	}
}
