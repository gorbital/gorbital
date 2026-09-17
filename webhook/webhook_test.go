package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/webhook"
)

// Test vectors computed outside Go, so the tests don't only check the
// verifier against itself.
var (
	// Svix's manual verification guide
	// (https://docs.svix.com/receiving/verifying-payloads/how-manual).
	svixSecret    = "whsec_plJ3nmyCDGBKInavdOK15jsl" // gitleaks:allow (published test vector)
	svixID        = "msg_loFOjxBNrRLzqYUf"
	svixTimestamp = "1731705121"
	svixBody      = `{"event_type":"ping","data":{"success":true}}`
	svixSignature = "v1,rAvfW3dJ/X/qxhsaXPOyyCGmRKsaKWcsNccKXlIktD0="

	// GitHub's guide to validating webhook deliveries
	// (https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).
	githubSecret    = []byte("It's a Secret to Everybody") // gitleaks:allow (published test vector)
	githubBody      = "Hello, World!"
	githubSignature = "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
)

// Keys for tests that sign their own requests.
var (
	keyA = []byte("0123456789abcdef0123456789abcdef")
	keyB = []byte("fedcba9876543210fedcba9876543210")
)

func standardSecret(key []byte) string { return "whsec_" + base64.StdEncoding.EncodeToString(key) }

func sign(key []byte, id, timestamp, body string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "." + body))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func standardHeaders(prefix, id, timestamp, signature string) http.Header {
	h := http.Header{}
	h.Set(prefix+"id", id)
	h.Set(prefix+"timestamp", timestamp)
	h.Set(prefix+"signature", signature)
	return h
}

func unix(ts string) time.Time {
	n, _ := strconv.ParseInt(ts, 10, 64)
	return time.Unix(n, 0)
}

func fixed(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestStandard(t *testing.T) {
	const id, ts, body = "msg_2mFz5wqS0bL9xRkq7vYt3Hn1Pd", "1789552800", `{"type":"payment.succeeded"}`
	now := unix(ts)
	good := sign(keyA, id, ts, body)
	tests := []struct {
		name    string
		secrets []string
		header  http.Header
		body    string
		now     time.Time
		want    error
	}{
		{"valid", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now, nil},
		{"secret without its prefix", []string{base64.StdEncoding.EncodeToString(keyA)}, standardHeaders("webhook-", id, ts, good), body, now, nil},
		{"rotated: old and new secrets, signed with the new", []string{standardSecret(keyB), standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now, nil},
		{"rotated: sender signs with both", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, sign(keyB, id, ts, body)+" "+good), body, now, nil},
		{"garbage entries around a valid one", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, "v1,AAAA v2,x "+good+" v1,not-base64 ,,,"), body, now, nil},
		{"4 minutes 59 seconds late", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now.Add(4*time.Minute + 59*time.Second), nil},
		{"4 minutes 59 seconds early", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now.Add(-4*time.Minute - 59*time.Second), nil},
		{"exactly 5 minutes late", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now.Add(5 * time.Minute), nil},

		{"expired", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now.Add(5*time.Minute + time.Second), webhook.ErrTimestamp},
		{"from the future", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body, now.Add(-5*time.Minute - time.Second), webhook.ErrTimestamp},
		{"tampered body", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), strings.Replace(body, "succeeded", "refunded", 1), now, webhook.ErrInvalidSignature},
		{"trailing newline", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, good), body + "\n", now, webhook.ErrInvalidSignature},
		{"wrong secret", []string{standardSecret(keyB)}, standardHeaders("webhook-", id, ts, good), body, now, webhook.ErrInvalidSignature},
		{"another delivery's ID", []string{standardSecret(keyA)}, standardHeaders("webhook-", "msg_other", ts, good), body, now, webhook.ErrInvalidSignature},
		{"timestamp moved within the window", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, "1789552801", good), body, now, webhook.ErrInvalidSignature},
		{"other version", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, "v2"+strings.TrimPrefix(good, "v1")), body, now, webhook.ErrInvalidSignature},
		{"no version", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, strings.TrimPrefix(good, "v1,")), body, now, webhook.ErrInvalidSignature},
		{"svix headers to a webhook- verifier", []string{standardSecret(keyA)}, standardHeaders("svix-", id, ts, good), body, now, webhook.ErrInvalidSignature},
		{"no signature", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, ts, ""), body, now, webhook.ErrInvalidSignature},
		{"no ID", []string{standardSecret(keyA)}, standardHeaders("webhook-", "", ts, good), body, now, webhook.ErrInvalidSignature},
		{"ID with a dot", []string{standardSecret(keyA)}, standardHeaders("webhook-", "msg.1", ts, good), body, now, webhook.ErrInvalidSignature},
		{"ID too long", []string{standardSecret(keyA)}, standardHeaders("webhook-", strings.Repeat("a", 256), ts, good), body, now, webhook.ErrInvalidSignature},
		{"no timestamp", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, "", good), body, now, webhook.ErrInvalidSignature},
		{"timestamp not in seconds", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, "2026-09-16T10:00:00Z", good), body, now, webhook.ErrInvalidSignature},
		{"negative timestamp", []string{standardSecret(keyA)}, standardHeaders("webhook-", id, "-1", good), body, now, webhook.ErrInvalidSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: tt.secrets, Now: fixed(tt.now)})
			if err != nil {
				t.Fatalf("NewStandard() error = %v", err)
			}
			err = v.Verify(context.Background(), tt.header, []byte(tt.body))
			if tt.want == nil && err != nil || tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("Verify() error = %v, want %v", err, tt.want)
			}
			if tt.want == webhook.ErrInvalidSignature && errors.Is(err, webhook.ErrTimestamp) {
				t.Errorf("Verify() error = %v, want a signature error, not a timestamp one", err)
			}
		})
	}
	if !errors.Is(webhook.ErrTimestamp, webhook.ErrInvalidSignature) {
		t.Error("ErrTimestamp doesn't wrap ErrInvalidSignature")
	}
}

func TestStandardSvixVector(t *testing.T) {
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{svixSecret}, HeaderPrefix: "svix-", Now: fixed(unix(svixTimestamp))})
	if err != nil {
		t.Fatalf("NewStandard() error = %v", err)
	}
	if err := v.Verify(context.Background(), standardHeaders("svix-", svixID, svixTimestamp, svixSignature), []byte(svixBody)); err != nil {
		t.Errorf("Verify(Svix's published vector) error = %v", err)
	}
}

func TestReorderedDeliveries(t *testing.T) {
	// Deliveries signed a minute apart arrive in the other order: each is
	// verified on its own, within the window.
	now := unix("1789552800")
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{standardSecret(keyA)}, Now: fixed(now)})
	if err != nil {
		t.Fatal(err)
	}
	first := strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10)
	second := strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)
	for _, d := range []struct{ id, ts string }{{"msg_2", second}, {"msg_1", first}} {
		body := `{"n":"` + d.id + `"}`
		if err := v.Verify(context.Background(), standardHeaders("webhook-", d.id, d.ts, sign(keyA, d.id, d.ts, body)), []byte(body)); err != nil {
			t.Errorf("Verify(%s) error = %v", d.id, err)
		}
	}
}

func TestNewStandardRefusesBadSecrets(t *testing.T) {
	for _, secrets := range [][]string{nil, {""}, {"whsec_"}, {"whsec_not base64!"}, {"whsec_c2hvcnQ="}, {standardSecret(keyA), "whsec_c2hvcnQ="}} {
		_, err := webhook.NewStandard(webhook.StandardConfig{Secrets: secrets})
		if err == nil {
			t.Errorf("NewStandard(%q) error = nil", secrets)
			continue
		}
		for _, s := range secrets {
			if enc := strings.TrimPrefix(s, "whsec_"); enc != "" && strings.Contains(err.Error(), enc) {
				t.Errorf("NewStandard() error quotes the secret: %v", err)
			}
		}
	}
}

func TestHMACGitHubVector(t *testing.T) {
	v, err := webhook.NewHMAC(webhook.HMACConfig{
		Secrets:         [][]byte{githubSecret},
		SignatureHeader: "X-Hub-Signature-256",
		SignaturePrefix: "sha256=",
		Encoding:        webhook.Hex,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{"X-Hub-Signature-256": {githubSignature}}
	if err := v.Verify(context.Background(), h, []byte(githubBody)); err != nil {
		t.Errorf("Verify(GitHub's published vector) error = %v", err)
	}
	h.Set("X-Hub-Signature-256", strings.ToUpper(githubSignature[len("sha256="):]))
	if err := v.Verify(context.Background(), h, []byte(githubBody)); !errors.Is(err, webhook.ErrInvalidSignature) {
		t.Errorf("Verify(without the sha256= prefix) error = %v", err)
	}
	h.Set("X-Hub-Signature-256", "sha256="+strings.ToUpper(githubSignature[len("sha256="):]))
	if err := v.Verify(context.Background(), h, []byte(githubBody)); err != nil {
		t.Errorf("Verify(upper-case hex) error = %v", err)
	}
	if err := v.Verify(context.Background(), h, []byte(githubBody+" ")); !errors.Is(err, webhook.ErrInvalidSignature) {
		t.Errorf("Verify(tampered body) error = %v", err)
	}
}

func TestHMACCustomSignedContent(t *testing.T) {
	// Slack signs "v0:<timestamp>:<body>" and sends "v0=<hex>".
	now := time.Unix(1789552800, 0)
	v, err := webhook.NewHMAC(webhook.HMACConfig{
		Secrets:         [][]byte{keyA},
		SignatureHeader: "X-Slack-Signature",
		SignaturePrefix: "v0=",
		Encoding:        webhook.Hex,
		TimestampHeader: "X-Slack-Request-Timestamp",
		Tolerance:       time.Minute,
		Signed: func(w io.Writer, _, timestamp string, body []byte) {
			_, _ = fmt.Fprintf(w, "v0:%s:", timestamp)
			_, _ = w.Write(body)
		},
		Now: fixed(now),
	})
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, keyA)
	mac.Write([]byte("v0:1789552800:token=x"))
	h := http.Header{}
	h.Set("X-Slack-Signature", fmt.Sprintf("v0=%x", mac.Sum(nil)))
	h.Set("X-Slack-Request-Timestamp", "1789552800")
	if err := v.Verify(context.Background(), h, []byte("token=x")); err != nil {
		t.Errorf("Verify() error = %v", err)
	}
	late, _ := webhook.NewHMAC(webhook.HMACConfig{Secrets: [][]byte{keyA}, SignatureHeader: "X-Slack-Signature", TimestampHeader: "X-Slack-Request-Timestamp", Tolerance: time.Minute, Now: fixed(now.Add(61 * time.Second))})
	if err := late.Verify(context.Background(), h, []byte("token=x")); !errors.Is(err, webhook.ErrTimestamp) {
		t.Errorf("Verify(61 s late, 1 min tolerance) error = %v, want ErrTimestamp", err)
	}
}

func TestNewHMACValidates(t *testing.T) {
	tests := map[string]webhook.HMACConfig{
		"no secret":          {SignatureHeader: "X-Signature"},
		"short secret":       {Secrets: [][]byte{[]byte("short")}, SignatureHeader: "X-Signature"},
		"no header":          {Secrets: [][]byte{keyA}},
		"unknown encoding":   {Secrets: [][]byte{keyA}, SignatureHeader: "X-Signature", Encoding: 7},
		"negative tolerance": {Secrets: [][]byte{keyA}, SignatureHeader: "X-Signature", Tolerance: -time.Second},
	}
	for name, cfg := range tests {
		if _, err := webhook.NewHMAC(cfg); err == nil {
			t.Errorf("%s: NewHMAC() error = nil", name)
		}
	}
	// The verifier keeps its own copy of the secrets.
	secret := append([]byte(nil), keyA...)
	v, err := webhook.NewHMAC(webhook.HMACConfig{Secrets: [][]byte{secret}, SignatureHeader: "X-Signature"})
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, keyA)
	mac.Write([]byte("body"))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	secret[0] ^= 0xff
	if err := v.Verify(context.Background(), http.Header{"X-Signature": {sig}}, []byte("body")); err != nil {
		t.Errorf("Verify() after the caller changed its secret slice: %v", err)
	}
}

func BenchmarkStandardVerify(b *testing.B) {
	const id, ts = "msg_2mFz5wqS0bL9xRkq7vYt3Hn1Pd", "1789552800"
	body := strings.Repeat(`{"type":"payment.succeeded"}`, 40)
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{standardSecret(keyA)}, Now: fixed(unix(ts))})
	if err != nil {
		b.Fatal(err)
	}
	h := standardHeaders("webhook-", id, ts, sign(keyA, id, ts, body))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := v.Verify(ctx, h, []byte(body)); err != nil {
			b.Fatal(err)
		}
	}
}
