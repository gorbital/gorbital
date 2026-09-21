package signintest

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/modules/auth/social"
)

// Clock skew thresholds: sign-in tolerates a minute on ID tokens, and
// Apple refuses client secrets issued in its future.
const (
	skewWarn = 10 * time.Second
	skewFail = time.Minute
)

// probeCode is the authorization code the credential check sends: no
// provider issues it, so a provider that accepts the client answers
// invalid_grant, and one that doesn't answers invalid_client first.
const probeCode = "gorbital-sign-in-test-not-a-code"

// NetworkChecks checks provider against the internet: its endpoints answer,
// this computer's clock agrees with the provider's, and the provider
// accepts the client credentials (a token request with a code no one
// issued, which a valid client gets refused as invalid_grant). It returns
// ErrNotConfigured.
func (t *Tester) NetworkChecks(ctx context.Context, provider string) (LiveChecks, error) {
	p := t.provider(provider)
	if p == nil {
		return LiveChecks{}, ErrNotConfigured
	}
	out := LiveChecks{Method: provider}
	reach := t.reachability(ctx, provider)
	out.Checks = append(out.Checks, reach...)
	out.Checks = append(out.Checks, t.credentialsCheck(ctx, provider, p))
	t.cfg.Logger.InfoContext(ctx, "sign-in network check", "method", provider)
	return out, nil
}

// reachability requests the provider's token endpoint, sign-in's own
// dependency, and compares the answer's Date with this computer's clock.
// Keys endpoints are answered from caches, whose Date is old (Google's are
// cached for hours).
func (t *Tester) reachability(ctx context.Context, provider string) []Check {
	target := ""
	switch provider {
	case social.Google:
		target = t.cfg.GoogleEndpoints.TokenURL
	case social.Apple:
		target = t.cfg.AppleEndpoints.TokenURL
	case social.GitHub:
		target = t.cfg.GitHubEndpoints.TokenURL
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return []Check{fail("provider_reachable", "the provider's address "+target+" is invalid", "", "")}
	}
	before := t.cfg.Now()
	resp, err := t.cfg.HTTPClient.Do(req)
	after := t.cfg.Now()
	if err != nil {
		return []Check{fail("provider_reachable", "couldn't reach "+providerLabel(provider)+" at "+target+": "+redact(err.Error()),
			"check this machine's internet connection, proxy and firewall", "")}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	checks := []Check{}
	// Any answer below 500 means the endpoint is there: a GET on a token
	// endpoint is refused.
	if resp.StatusCode >= 500 {
		checks = append(checks, warn("provider_reachable", providerLabel(provider)+" answered "+resp.Status+" at "+target, "try again later; the provider may be having trouble", ""))
	} else {
		checks = append(checks, ok("provider_reachable", providerLabel(provider)+" answers at "+target+" ("+after.Sub(before).Round(time.Millisecond).String()+")"))
	}
	return append(checks, clockCheck(provider, resp.Header.Get("Date"), resp.Header.Get("Age"), before, after))
}

// clockCheck compares a response's Date header, plus its Age when a cache
// answered, with the local time halfway through the request. Date has
// one-second precision.
func clockCheck(provider, date, age string, before, after time.Time) Check {
	remote, err := http.ParseTime(date)
	if seconds, aerr := strconv.Atoi(strings.TrimSpace(age)); err == nil && aerr == nil && seconds > 0 {
		remote = remote.Add(time.Duration(seconds) * time.Second)
	}
	if err != nil {
		return Check{Code: "clock_skew", Status: StatusSkip, Message: providerLabel(provider) + " sent no usable Date header, so the clock couldn't be compared"}
	}
	local := before.Add(after.Sub(before) / 2)
	skew := local.Sub(remote)
	abs := time.Duration(math.Abs(float64(skew)))
	ahead := "ahead of"
	if skew < 0 {
		ahead = "behind"
	}
	msg := fmt.Sprintf("this computer's clock is %s %s %s's", abs.Round(time.Second), ahead, providerLabel(provider))
	fix := "turn on automatic date and time (NTP) on this computer"
	switch {
	case abs >= skewFail:
		return fail("clock_skew", msg+": ID tokens look issued in the future or past, and Apple refuses client secrets from the future", fix, "")
	case abs >= skewWarn:
		return warn("clock_skew", msg+"; sign-in tolerates up to "+skewFail.String(), fix, "")
	}
	return ok("clock_skew", fmt.Sprintf("this computer's clock agrees with %s's (within %s)", providerLabel(provider), (abs+time.Second).Round(time.Second)))
}

// credentialsCheck sends the configured client ID and secret (for Apple, a
// client secret signed with the .p8 key) to the token endpoint with a code
// no one issued.
func (t *Tester) credentialsCheck(ctx context.Context, provider string, p *social.Provider) Check {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var err error
	switch {
	case p.Web():
		_, err = p.Exchange(ctx, t.CallbackURL(provider), probeCode, social.NewPKCEVerifier(), "nonce")
	case provider == social.Apple && len(t.cfg.App.Auth.AppleBundleIDs) > 0:
		_, err = p.ExchangeNativeCode(ctx, probeCode, t.cfg.App.Auth.AppleBundleIDs[0])
	default:
		return Check{Code: "client_credentials", Status: StatusSkip, Message: providerLabel(provider) + " has no client secret to check here (native clients only)"}
	}
	if err == nil {
		return warn("client_credentials", providerLabel(provider)+" accepted a code no one issued", "this isn't "+providerLabel(provider)+"'s token endpoint; check the provider's endpoints", "")
	}
	f := t.classify(provider, err, probeCode)
	switch f.code {
	case "invalid_grant":
		return ok("client_credentials", providerLabel(provider)+" accepts the client credentials (it refused only the made-up code, as expected)", credentialVariables(provider)...)
	case "invalid_client", "redirect_uri_mismatch", "provider_unreachable":
		c := fail("client_credentials", f.message, f.fix, f.link, credentialVariables(provider)...)
		return c
	}
	return warn("client_credentials", providerLabel(provider)+" answered in a way the check doesn't recognise: "+f.message, f.fix, f.link, credentialVariables(provider)...)
}

func credentialVariables(provider string) []string {
	switch provider {
	case social.Google:
		return []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"}
	case social.Apple:
		return []string{"APPLE_TEAM_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY_FILE", "APPLE_SERVICES_ID"}
	case social.GitHub:
		return []string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"}
	}
	return nil
}
