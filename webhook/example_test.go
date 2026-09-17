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
	"time"

	"gorbital.dev/webhook"
)

// exampleSecret is a Standard Webhooks secret for the examples.
const exampleSecret = "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // gitleaks:allow (example)

// signed returns the headers a Standard Webhooks sender sets for body.
func signed(id string, at time.Time, body string) http.Header {
	key, _ := webhook.DecodeStandardSecret(exampleSecret)
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	h := http.Header{}
	h.Set("webhook-id", id)
	h.Set("webhook-timestamp", ts)
	h.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

func ExampleNewStandard() {
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{exampleSecret}})
	if err != nil {
		panic(err)
	}
	body := `{"type":"payment.succeeded"}`
	header := signed("msg_1", time.Now(), body)

	fmt.Println(v.Verify(context.Background(), header, []byte(body)))
	err = v.Verify(context.Background(), header, []byte(`{"type":"payment.refunded"}`))
	fmt.Println(errors.Is(err, webhook.ErrInvalidSignature))
	// Output:
	// <nil>
	// true
}

func ExampleNewStandard_svix() {
	// Resend and Clerk send Svix's header names.
	v, err := webhook.NewStandard(webhook.StandardConfig{
		Secrets:      []string{"whsec_plJ3nmyCDGBKInavdOK15jsl"}, // gitleaks:allow (Svix's published test vector)
		HeaderPrefix: "svix-",
		Now:          func() time.Time { return time.Unix(1731705121, 0) },
	})
	if err != nil {
		panic(err)
	}
	header := http.Header{}
	header.Set("svix-id", "msg_loFOjxBNrRLzqYUf")
	header.Set("svix-timestamp", "1731705121")
	header.Set("svix-signature", "v1,rAvfW3dJ/X/qxhsaXPOyyCGmRKsaKWcsNccKXlIktD0=")
	fmt.Println(v.Verify(context.Background(), header, []byte(`{"event_type":"ping","data":{"success":true}}`)))
	// Output: <nil>
}

func ExampleStandardConfig() {
	// During a rotation, accept the old and the new secret.
	v, err := webhook.NewStandard(webhook.StandardConfig{
		Secrets: []string{
			"whsec_ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=", // gitleaks:allow (example: the new secret)
			exampleSecret, // the old secret
		},
		Tolerance: 3 * time.Minute,
	})
	if err != nil {
		panic(err)
	}
	body := `{}`
	fmt.Println(v.Verify(context.Background(), signed("msg_1", time.Now(), body), []byte(body)))
	err = v.Verify(context.Background(), signed("msg_2", time.Now().Add(-4*time.Minute), body), []byte(body))
	fmt.Println(errors.Is(err, webhook.ErrTimestamp))
	// Output:
	// <nil>
	// true
}

func ExampleNewHMAC() {
	// GitHub: "X-Hub-Signature-256: sha256=<hex>" over the body alone.
	v, err := webhook.NewHMAC(webhook.HMACConfig{
		Secrets:         [][]byte{[]byte("It's a Secret to Everybody")}, // gitleaks:allow (GitHub's published test vector)
		SignatureHeader: "X-Hub-Signature-256",
		SignaturePrefix: "sha256=",
		Encoding:        webhook.Hex,
	})
	if err != nil {
		panic(err)
	}
	header := http.Header{}
	header.Set("X-Hub-Signature-256", "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17")
	fmt.Println(v.Verify(context.Background(), header, []byte("Hello, World!")))
	// Output: <nil>
}

func ExampleHMACConfig() {
	// Slack signs "v0:<timestamp>:<body>" and sends "v0=<hex>".
	v, err := webhook.NewHMAC(webhook.HMACConfig{
		Secrets:         [][]byte{[]byte("8f742231b10e8888abcd99yyyzzz85a5")}, // gitleaks:allow (example)
		SignatureHeader: "X-Slack-Signature",
		SignaturePrefix: "v0=",
		Encoding:        webhook.Hex,
		TimestampHeader: "X-Slack-Request-Timestamp",
		Signed: func(w io.Writer, _, timestamp string, body []byte) {
			_, _ = io.WriteString(w, "v0:"+timestamp+":")
			_, _ = w.Write(body)
		},
	})
	if err != nil {
		panic(err)
	}
	header := http.Header{}
	header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	header.Set("X-Slack-Signature", "v0=0000")
	fmt.Println(errors.Is(v.Verify(context.Background(), header, []byte("token=x")), webhook.ErrInvalidSignature))
	// Output: true
}

func ExampleHMAC_Verify() {
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{exampleSecret}})
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/payments", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if err := v.Verify(r.Context(), r.Header, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Act on the event, idempotently: retries keep the webhook-id.
		w.WriteHeader(http.StatusNoContent)
	})
}

func ExampleDecodeStandardSecret() {
	_, err := webhook.DecodeStandardSecret("whsec_c2hvcnQ=")
	fmt.Println(err)
	// Output: webhook: the signing secret must be whsec_ followed by base64 of at least 16 bytes
}

func ExampleVerifier() {
	// Any type with a Verify method can guard a route, such as a verifier
	// for a sender that signs with a public key.
	var v webhook.Verifier = verifierFunc(func(_ context.Context, header http.Header, _ []byte) error {
		if header.Get("X-Test-Signature") != "ok" {
			return webhook.ErrInvalidSignature
		}
		return nil
	})
	fmt.Println(v.Verify(context.Background(), http.Header{"X-Test-Signature": {"ok"}}, nil))
	// Output: <nil>
}

type verifierFunc func(ctx context.Context, header http.Header, body []byte) error

func (f verifierFunc) Verify(ctx context.Context, header http.Header, body []byte) error {
	return f(ctx, header, body)
}
