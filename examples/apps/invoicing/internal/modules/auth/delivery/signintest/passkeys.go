package signintest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
)

// PasskeyPagePath is where the app serves the ceremony page in development.
// It does nothing without a ticket from POST /_dev/auth/test/passkeys/start,
// which only the dev console hands out.
const PasskeyPagePath = "/_signin-test/passkey" //nolint:gosec // a path, not a credential

// passkeyTest is a ceremony test: a throwaway user and, after the
// registration step, its throwaway credential, both only in memory.
type passkeyTest struct {
	id        string
	resultURL string
	origin    string
	expiresAt time.Time
	step      string // "register", then "login", then "done"
	user      passkey.User
	state     []byte // the started step's ceremony state
}

// StartPasskey starts a ceremony test on the app's origin: it returns the
// page URL to open, whose fragment carries the ticket (never sent to a
// server by the browser, nor in a Referer). It returns ErrNotConfigured, an
// *UnavailableError, ErrInvalidResultURL or ErrTooManyTests.
func (t *Tester) StartPasskey(resultURL string) (Start, error) {
	if t.cfg.Passkeys == nil {
		return Start{}, ErrNotConfigured
	}
	if u := t.passkeysUnavailable(); u != nil {
		return Start{}, u
	}
	resultURL, err := CheckResultURL(resultURL)
	if err != nil {
		return Start{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	if len(t.passkeys) >= MaxPending {
		return Start{}, ErrTooManyTests
	}
	now := t.cfg.Now()
	ticket, hash := newSecret()
	handle := make([]byte, 32)
	_, _ = rand.Read(handle) // never fails (crypto/rand)
	p := &passkeyTest{
		id: authlib.NewID("slt"), resultURL: resultURL, origin: t.passkeyOrigin(), expiresAt: now.Add(PasskeyTTL), step: "register",
		user: passkey.User{Handle: handle, Name: "passkey-test", DisplayName: "gorbital passkey test (safe to delete)"},
	}
	t.passkeys[hash] = p
	t.addResult(&Result{ID: p.id, Method: MethodPasskeys, Kind: "ceremony", State: StatePending, StartedAt: now, ExpiresAt: p.expiresAt})
	return Start{ID: p.id, URL: p.origin + PasskeyPagePath + "#ticket=" + ticket, ExpiresAt: p.expiresAt}, nil
}

// passkeyRequest is what the ceremony page posts.
type passkeyRequest struct {
	Ticket string `json:"ticket"`
	Step   string `json:"step"`
	// Response is the browser's credential, as JSON.
	Response json.RawMessage `json:"response,omitempty"`
	// Error is the browser's DOMException when the ceremony failed there.
	Error *struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// passkeyAnswer is what the ceremony page gets back.
type passkeyAnswer struct {
	// Options are for navigator.credentials (options), or Next names the
	// next step (finish): login, or done with Redirect.
	Options  json.RawMessage `json:"options,omitempty"`
	Next     string          `json:"next,omitempty"`
	Redirect string          `json:"redirect,omitempty"`
	RPID     string          `json:"rp_id,omitempty"`
	Code     string          `json:"code,omitempty"`
	Message  string          `json:"message,omitempty"`
}

// PasskeyHandler serves the ceremony page and its two endpoints under
// PasskeyPagePath. Mount it for GET PasskeyPagePath and POST
// PasskeyPagePath + "/options" and "/finish".
func (t *Tester) PasskeyHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PasskeyPagePath, t.servePasskeyPage)
	mux.HandleFunc("POST "+PasskeyPagePath+"/options", t.passkeyOptions)
	mux.HandleFunc("POST "+PasskeyPagePath+"/finish", t.passkeyFinish)
	return mux
}

// lookupPasskey returns the ceremony of ticket. The caller holds t.mu.
func (t *Tester) lookupPasskey(ticket string) (*passkeyTest, [sha256.Size]byte, bool) {
	if ticket == "" || len(ticket) > 128 {
		return nil, [sha256.Size]byte{}, false
	}
	hash := sha256.Sum256([]byte(ticket))
	p, ok := t.passkeys[hash]
	if !ok || p.step == "done" || !t.cfg.Now().Before(p.expiresAt) {
		return nil, hash, false
	}
	return p, hash, true
}

func readPasskeyRequest(w http.ResponseWriter, r *http.Request) (passkeyRequest, bool) {
	var req passkeyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, passkeyAnswer{Code: "invalid_request", Message: "the request isn't JSON"})
		return req, false
	}
	return req, true
}

func (t *Tester) passkeyOptions(w http.ResponseWriter, r *http.Request) {
	req, ok := readPasskeyRequest(w, r)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	p, _, ok := t.lookupPasskey(req.Ticket)
	if !ok {
		writeJSON(w, http.StatusNotFound, passkeyAnswer{Code: "test_not_found", Message: ErrTestNotFound.Error()})
		return
	}
	if req.Step != p.step {
		writeJSON(w, http.StatusConflict, passkeyAnswer{Code: "invalid_request", Message: "the ceremony is at step " + p.step})
		return
	}
	var (
		c   passkey.Ceremony
		err error
	)
	if p.step == "register" {
		c, err = t.cfg.Passkeys.BeginRegistration(p.user) // as adding a passkey does
	} else {
		c, err = t.cfg.Passkeys.BeginUserLogin(p.user) // limited to the test's passkey
	}
	if err != nil {
		t.failPasskey(r, p, "passkey_invalid", "the ceremony couldn't start: "+redact(err.Error()), "")
		writeJSON(w, http.StatusOK, passkeyAnswer{Next: "done", Redirect: resultRedirect(p.resultURL, p.id, MethodPasskeys)})
		return
	}
	p.state = c.State
	writeJSON(w, http.StatusOK, passkeyAnswer{Options: c.Options, RPID: t.cfg.Passkeys.Config().RPID})
}

func (t *Tester) passkeyFinish(w http.ResponseWriter, r *http.Request) {
	req, ok := readPasskeyRequest(w, r)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	p, _, ok := t.lookupPasskey(req.Ticket)
	if !ok {
		writeJSON(w, http.StatusNotFound, passkeyAnswer{Code: "test_not_found", Message: ErrTestNotFound.Error()})
		return
	}
	done := passkeyAnswer{Next: "done", Redirect: resultRedirect(p.resultURL, p.id, MethodPasskeys)}
	switch {
	case req.Step != p.step:
		writeJSON(w, http.StatusConflict, passkeyAnswer{Code: "invalid_request", Message: "the ceremony is at step " + p.step})
		return
	case req.Error != nil:
		code, msg, fix := t.browserFailure(p, req.Error.Name, redact(req.Error.Message))
		t.failPasskey(r, p, code, msg, fix)
		writeJSON(w, http.StatusOK, done)
		return
	case p.state == nil || len(req.Response) == 0:
		writeJSON(w, http.StatusConflict, passkeyAnswer{Code: "invalid_request", Message: "get the step's options first, then send the browser's response"})
		return
	}
	state := p.state
	p.state = nil // each step's challenge answers once
	if p.step == "register" {
		cred, err := t.cfg.Passkeys.FinishRegistration(p.user, state, req.Response)
		if err != nil {
			code, msg, fix := t.serverFailure(p, "registration", err)
			t.failPasskey(r, p, code, msg, fix)
			writeJSON(w, http.StatusOK, done)
			return
		}
		p.user.Credentials = []passkey.Credential{cred}
		p.step = "login"
		writeJSON(w, http.StatusOK, passkeyAnswer{Next: "login"})
		return
	}
	_, cred, err := t.cfg.Passkeys.FinishLogin(state, req.Response, func(handle, credentialID []byte) (passkey.User, error) {
		if string(handle) != string(p.user.Handle) || len(p.user.Credentials) == 0 || string(credentialID) != string(p.user.Credentials[0].ID) {
			return passkey.User{}, passkey.ErrUnknownCredential
		}
		return p.user, nil
	})
	if err != nil && !errors.Is(err, passkey.ErrCloneWarning) {
		code, msg, fix := t.serverFailure(p, "sign-in", err)
		t.failPasskey(r, p, code, msg, fix)
		writeJSON(w, http.StatusOK, done)
		return
	}
	p.step = "done"
	p.user.Credentials = nil // forget the throwaway passkey
	if res := t.results[p.id]; res != nil {
		t.finish(res, StatePassed, "ok", "passkeys work on "+p.origin+" for relying party "+t.cfg.Passkeys.Config().RPID+": a passkey was created and used, with user verification, and verified as sign-in does", "", "")
		res.Passkey = &PasskeyInfo{
			RPID: t.cfg.Passkeys.Config().RPID, Origin: p.origin, CredentialID: base64.RawURLEncoding.EncodeToString(cred.ID),
			BackupEligible: cred.BackupEligible, BackupState: cred.BackupState, UserVerified: true,
		}
	}
	t.cfg.Logger.InfoContext(r.Context(), "sign-in test finished", "method", MethodPasskeys, "state", StatePassed, "code", "ok")
	done.RPID = t.cfg.Passkeys.Config().RPID
	writeJSON(w, http.StatusOK, done)
}

// failPasskey ends a ceremony test with a failure. The caller holds t.mu.
func (t *Tester) failPasskey(r *http.Request, p *passkeyTest, code, message, fix string) {
	p.step, p.state, p.user.Credentials = "done", nil, nil
	if res := t.results[p.id]; res != nil {
		link := ""
		if code == "rp_id_mismatch" || code == "origin_not_allowed" {
			link = LinkEnvironment
		}
		t.finish(res, StateFailed, code, message, fix, link)
	}
	t.cfg.Logger.InfoContext(r.Context(), "sign-in test finished", "method", MethodPasskeys, "state", StateFailed, "code", code)
}

// browserFailure explains a DOMException from navigator.credentials.
func (t *Tester) browserFailure(p *passkeyTest, name, message string) (code, msg, fix string) {
	rp := t.cfg.Passkeys.Config().RPID
	switch name {
	case "SecurityError":
		return "rp_id_mismatch", "the browser refused relying party " + rp + " on " + p.origin + ": " + message,
			"WEBAUTHN_RP_ID must be the page's host or a parent domain of it (" + p.origin + "), and the page must be https or http://localhost"
	case "NotAllowedError":
		return "passkey_cancelled", "the passkey prompt was cancelled or timed out: " + message,
			"try again and complete the prompt; if it closes at once, the browser may not allow passkeys here (for example on 127.0.0.1 instead of localhost)"
	case "NotSupportedError", "Unsupported":
		return "passkey_unsupported", "this browser can't create the passkey sign-in asks for (a discoverable credential with user verification): " + message,
			"use a browser and device with passkeys, such as a current Chrome, Safari or Firefox with a platform authenticator or password manager"
	case "InvalidStateError":
		return "passkey_invalid", "the authenticator refused: " + message, "try again; remove old test passkeys from the password manager if it persists"
	}
	return "passkey_invalid", "the browser failed the ceremony (" + name + "): " + message, "try again in another browser"
}

// serverFailure explains a verification error from the passkey package.
func (t *Tester) serverFailure(p *passkeyTest, step string, err error) (code, msg, fix string) {
	detail := redact(strings.TrimPrefix(err.Error(), passkey.ErrInvalidResponse.Error()+": "))
	lower := strings.ToLower(detail)
	rp := t.cfg.Passkeys.Config().RPID
	switch {
	case strings.Contains(lower, "origin"):
		return "origin_not_allowed", "the " + step + " came from an origin WEBAUTHN_ORIGINS doesn't allow: " + detail,
			"add the origin the browser reports to WEBAUTHN_ORIGINS; the page ran on " + p.origin
	case strings.Contains(lower, "rp hash") || strings.Contains(lower, "rpid") || strings.Contains(lower, "rp id"):
		return "rp_id_mismatch", "the passkey was made for another relying party than " + rp + ": " + detail, "check WEBAUTHN_RP_ID is " + p.origin + "'s domain"
	case strings.Contains(lower, "verif") && strings.Contains(lower, "user"):
		return "passkey_invalid", "the authenticator didn't verify the person (PIN, fingerprint or face), which sign-in requires: " + detail,
			"use an authenticator with user verification, and complete its prompt"
	}
	return "passkey_invalid", "the " + step + " response doesn't verify: " + detail, "try again; if it persists, try another browser or authenticator"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// servePasskeyPage serves the ceremony page. It holds nothing secret: the
// ticket is in the URL's fragment, which only the page's script reads.
func (t *Tester) servePasskeyPage(w http.ResponseWriter, _ *http.Request) {
	nonce := make([]byte, 18)
	_, _ = rand.Read(nonce) // never fails (crypto/rand)
	n := base64.StdEncoding.EncodeToString(nonce)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+n+"'; style-src 'nonce-"+n+"'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	_ = passkeyPage.Execute(w, map[string]any{"Nonce": n, "Path": PasskeyPagePath})
}

var passkeyPage = template.Must(template.New("passkey").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Passkey test</title>
<style nonce="{{.Nonce}}">
:root { color-scheme: light dark; --bg: #fafafa; --fg: #18181b; --muted: #71717a; --ok: #15803d; --bad: #b91c1c; }
@media (prefers-color-scheme: dark) { :root { --bg: #0b0b0e; --fg: #f4f4f5; --muted: #a1a1aa; --ok: #4ade80; --bad: #f87171; } }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: var(--bg); color: var(--fg); font: 14px/1.5 ui-sans-serif, system-ui, sans-serif; }
main { max-width: 440px; padding: 24px; }
h1 { font-size: 16px; margin: 0 0 8px; }
p { margin: 8px 0; color: var(--muted); }
#status { color: var(--fg); }
.ok { color: var(--ok) !important; } .bad { color: var(--bad) !important; }
code { font: 12px ui-monospace, monospace; }
</style>
</head>
<body>
<main>
<h1>Passkey test</h1>
<p id="status">Starting…</p>
<p>The browser creates a throwaway passkey named <code>gorbital passkey test (safe to delete)</code> and signs in with it once. The app never stores it; this page asks the browser to forget it afterwards when the browser supports that.</p>
</main>
<script nonce="{{.Nonce}}">
(() => {
  const path = {{.Path}};
  const status = document.getElementById("status");
  const say = (text, cls) => { status.textContent = text; status.className = cls || ""; };
  const ticket = new URLSearchParams(location.hash.slice(1)).get("ticket");
  history.replaceState(null, "", location.pathname);
  const b64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  const unb64 = (s) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4)), (c) => c.charCodeAt(0)).buffer;
  const post = async (step, body) => {
    const res = await fetch(path + "/" + step, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ticket, ...body }), credentials: "omit" });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw Object.assign(new Error(data.message || res.statusText), { server: true });
    return data;
  };
  const creationOptions = (o) => {
    const pk = o.publicKey;
    if (window.PublicKeyCredential && PublicKeyCredential.parseCreationOptionsFromJSON) return PublicKeyCredential.parseCreationOptionsFromJSON(pk);
    return { ...pk, challenge: unb64(pk.challenge), user: { ...pk.user, id: unb64(pk.user.id) }, excludeCredentials: (pk.excludeCredentials || []).map((c) => ({ ...c, id: unb64(c.id) })) };
  };
  const requestOptions = (o) => {
    const pk = o.publicKey;
    if (window.PublicKeyCredential && PublicKeyCredential.parseRequestOptionsFromJSON) return PublicKeyCredential.parseRequestOptionsFromJSON(pk);
    return { ...pk, challenge: unb64(pk.challenge), allowCredentials: (pk.allowCredentials || []).map((c) => ({ ...c, id: unb64(c.id) })) };
  };
  const credentialJSON = (c) => {
    if (typeof c.toJSON === "function") return c.toJSON();
    const r = c.response, out = { id: c.id, rawId: b64(c.rawId), type: c.type, clientExtensionResults: c.getClientExtensionResults(), authenticatorAttachment: c.authenticatorAttachment || undefined, response: { clientDataJSON: b64(r.clientDataJSON) } };
    if (r.attestationObject) { out.response.attestationObject = b64(r.attestationObject); out.response.transports = r.getTransports ? r.getTransports() : []; }
    else { out.response.authenticatorData = b64(r.authenticatorData); out.response.signature = b64(r.signature); if (r.userHandle) out.response.userHandle = b64(r.userHandle); }
    return out;
  };
  const failed = async (step, err) => {
    const data = await post("finish", { step, error: { name: err.name || "Error", message: String(err.message || err) } });
    location.replace(data.redirect);
  };
  const run = async () => {
    if (!ticket) { say("This page runs a passkey test started from the Dev Portal. Start it there.", "bad"); return; }
    if (!window.PublicKeyCredential || !navigator.credentials) {
      await failed("register", { name: "Unsupported", message: "this browser has no WebAuthn (navigator.credentials) on " + location.origin });
      return;
    }
    let step = "register", credentialId = null, rpId = null;
    try {
      say("Creating a throwaway passkey…");
      const reg = await post("options", { step });
      rpId = reg.rp_id;
      const created = await navigator.credentials.create({ publicKey: creationOptions(reg.options) });
      credentialId = created.id;
      const next = await post("finish", { step, response: credentialJSON(created) });
      if (next.next === "done") { location.replace(next.redirect); return; }
      step = "login";
      say("Signing in with it…");
      const login = await post("options", { step });
      const got = await navigator.credentials.get({ publicKey: requestOptions(login.options) });
      const done = await post("finish", { step, response: credentialJSON(got) });
      say("Done. Returning to the Dev Portal…", "ok");
      if (credentialId && rpId && PublicKeyCredential.signalUnknownCredential) {
        try { await PublicKeyCredential.signalUnknownCredential({ rpId, credentialId }); } catch (e) { /* not every browser forgets it */ }
      }
      location.replace(done.redirect);
    } catch (err) {
      if (err && err.server) { say(err.message, "bad"); return; }
      say("The browser refused: " + (err && err.name) + ". Reporting…", "bad");
      try { await failed(step, err); } catch (e) { say("The test couldn't report: " + e.message, "bad"); }
    }
  };
  run();
})();
</script>
</body>
</html>
`))
