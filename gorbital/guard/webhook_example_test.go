package guard_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/webhook"
)

type paymentEvent struct {
	Body struct {
		Type string `json:"type"`
	}
}

func paymentReceived(_ context.Context, in *paymentEvent) (*struct{}, error) {
	fmt.Println("handled", in.Body.Type)
	return nil, nil
}

// deliver posts body with header and returns the status and code.
func (s server) deliver(target string, header http.Header, body string) string {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		return fmt.Sprintf("%d %s", rec.Code, problemCode(rec.Body.Bytes()))
	}
	return fmt.Sprint(rec.Code)
}

func ExampleWebhook() {
	payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
	if err != nil {
		panic(err)
	}
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/payments", paymentReceived, guard.Public(), guard.Webhook(payments))
	})
	body := `{"type":"payment.succeeded"}`
	fmt.Println(s.deliver("/v1/webhooks/payments", signWebhook(webhookKey, "msg_1", time.Now(), body), body))
	fmt.Println(s.deliver("/v1/webhooks/payments", signWebhook(webhookKey, "msg_1", time.Now(), body), `{"type":"payment.refunded"}`))
	// Output:
	// handled payment.succeeded
	// 204
	// 401 invalid_webhook_signature
}

func ExampleWebhookBodyLimit() {
	payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
	if err != nil {
		panic(err)
	}
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/payments", paymentReceived, guard.Public(), guard.Webhook(payments, guard.WebhookBodyLimit(16)))
	})
	body := `{"type":"payment.succeeded"}`
	fmt.Println(s.deliver("/v1/webhooks/payments", signWebhook(webhookKey, "msg_1", time.Now(), body), body))
	// Output:
	// 413 request_too_large
}

func ExampleWebhookOption() {
	// Options follow the verifier.
	github, err := webhook.NewHMAC(webhook.HMACConfig{
		Secrets:         [][]byte{[]byte("It's a Secret to Everybody")}, // gitleaks:allow (GitHub's published test vector)
		SignatureHeader: "X-Hub-Signature-256",
		SignaturePrefix: "sha256=",
		Encoding:        webhook.Hex,
	})
	if err != nil {
		panic(err)
	}
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/github", paymentReceived, guard.Public(), guard.Webhook(github, guard.WebhookBodyLimit(25<<20)))
	})
	header := http.Header{"X-Hub-Signature-256": {"sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"}}
	// The signature is for "Hello, World!", not this body.
	fmt.Println(s.deliver("/v1/webhooks/github", header, `{"type":"push"}`))
	// Output:
	// 401 invalid_webhook_signature
}
