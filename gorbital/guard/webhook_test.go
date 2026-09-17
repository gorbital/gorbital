package guard_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/webhook"
)

var webhookKey = []byte("0123456789abcdef0123456789abcdef")

const webhookSecret = "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // gitleaks:allow (test key)

type rawInput struct {
	RawBody []byte
}

type eventInput struct {
	Body struct {
		Type string `json:"type" minLength:"1"`
	}
}

type receivedOutput struct {
	Body struct {
		Received string `json:"received"`
	}
}

func signWebhook(key []byte, id string, at time.Time, body string) http.Header {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	h := http.Header{}
	h.Set("webhook-id", id)
	h.Set("webhook-timestamp", ts)
	h.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

func postWebhook(t *testing.T, s server, target string, header http.Header, body io.Reader, contentLength int64) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, body)
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	if contentLength != 0 {
		req.ContentLength = contentLength
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	var p struct {
		Code     string `json:"code"`
		Received string `json:"received"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return rec.Code, p.Code, p.Received
}

// failingReader fails after returning its data, like a client that
// disconnects mid-body.
type failingReader struct{ data *strings.Reader }

func (f failingReader) Read(p []byte) (int, error) {
	if f.data.Len() == 0 {
		return 0, errors.New("connection reset")
	}
	return f.data.Read(p)
}

func TestWebhook(t *testing.T) {
	now := time.Now()
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	s := mount(t, routes(func(r *gorbital.Router) {
		hooks := r.Group("/v1/webhooks", guard.Public())
		gorbital.Post(hooks, "/raw", func(_ context.Context, in *rawInput) (*receivedOutput, error) {
			out := &receivedOutput{}
			out.Body.Received = string(in.RawBody)
			return out, nil
		}, guard.Webhook(v, guard.WebhookBodyLimit(64)))
		gorbital.Post(hooks, "/events", func(_ context.Context, in *eventInput) (*receivedOutput, error) {
			out := &receivedOutput{}
			out.Body.Received = in.Body.Type
			return out, nil
		}, guard.Webhook(v))
	}))

	const body = `{"type":"payment.succeeded"}`
	good := signWebhook(webhookKey, "msg_1", now, body)
	otherKey := []byte("fedcba9876543210fedcba9876543210")
	big := `{"type":"` + strings.Repeat("x", 64) + `"}`

	tests := []struct {
		name          string
		target        string
		header        http.Header
		body          io.Reader
		contentLength int64
		wantStatus    int
		wantCode      string
		wantReceived  string
	}{
		{"valid, raw body reaches the handler", "/v1/webhooks/raw", good, strings.NewReader(body), 0, 200, "", body},
		{"valid, parsed body reaches the handler", "/v1/webhooks/events", good, strings.NewReader(body), 0, 200, "", "payment.succeeded"},
		{"tampered body", "/v1/webhooks/events", good, strings.NewReader(`{"type":"payment.refunded"}`), 0, 401, "invalid_webhook_signature", ""},
		{"wrong secret", "/v1/webhooks/events", signWebhook(otherKey, "msg_1", now, body), strings.NewReader(body), 0, 401, "invalid_webhook_signature", ""},
		{"expired", "/v1/webhooks/events", signWebhook(webhookKey, "msg_1", now.Add(-6*time.Minute), body), strings.NewReader(body), 0, 401, "invalid_webhook_signature", ""},
		{"from the future", "/v1/webhooks/events", signWebhook(webhookKey, "msg_1", now.Add(6*time.Minute), body), strings.NewReader(body), 0, 401, "invalid_webhook_signature", ""},
		{"missing headers", "/v1/webhooks/events", http.Header{}, strings.NewReader(body), 0, 401, "invalid_webhook_signature", ""},
		{"oversized, declared", "/v1/webhooks/raw", signWebhook(webhookKey, "msg_1", now, big), strings.NewReader(big), 0, 413, "request_too_large", ""},
		{"oversized, undeclared length", "/v1/webhooks/raw", signWebhook(webhookKey, "msg_1", now, big), io.MultiReader(strings.NewReader(big)), -1, 413, "request_too_large", ""},
		{"body cut off", "/v1/webhooks/raw", good, failingReader{strings.NewReader(body[:5])}, -1, 400, "bad_request", ""},
		// A signed body that fails validation is refused by validation,
		// after the signature check.
		{"signed but invalid input", "/v1/webhooks/events", signWebhook(webhookKey, "msg_1", now, `{"type":""}`), strings.NewReader(`{"type":""}`), 0, 422, "validation_failed", ""},
		{"unsigned invalid input never reaches validation", "/v1/webhooks/events", http.Header{}, strings.NewReader(`{"type":""}`), 0, 401, "invalid_webhook_signature", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, received := postWebhook(t, s, tt.target, tt.header, tt.body, tt.contentLength)
			if status != tt.wantStatus || code != tt.wantCode || received != tt.wantReceived {
				t.Errorf("POST %s = %d %q received %q, want %d %q received %q", tt.target, status, code, received, tt.wantStatus, tt.wantCode, tt.wantReceived)
			}
		})
	}
}

func TestWebhookRotatedSecretAndReorderedDeliveries(t *testing.T) {
	now := time.Now()
	newKey := []byte("fedcba9876543210fedcba9876543210")
	v, err := webhook.NewStandard(webhook.StandardConfig{
		Secrets: []string{"whsec_" + base64.StdEncoding.EncodeToString(newKey), webhookSecret},
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/events", func(_ context.Context, in *eventInput) (*receivedOutput, error) {
			out := &receivedOutput{}
			out.Body.Received = in.Body.Type
			return out, nil
		}, guard.Public(), guard.Webhook(v))
	}))
	// Signed with the old key a minute after one signed with the new key,
	// delivered in the other order.
	deliveries := []struct {
		key  []byte
		id   string
		at   time.Time
		body string
	}{
		{webhookKey, "msg_2", now.Add(-time.Minute), `{"type":"second"}`},
		{newKey, "msg_1", now.Add(-2 * time.Minute), `{"type":"first"}`},
	}
	for _, d := range deliveries {
		status, code, received := postWebhook(t, s, "/v1/webhooks/events", signWebhook(d.key, d.id, d.at, d.body), strings.NewReader(d.body), 0)
		if status != 200 || received == "" {
			t.Errorf("delivery %s = %d %q %q, want 200", d.id, status, code, received)
		}
	}
}

func TestWebhookVerifierFailure(t *testing.T) {
	down := errors.New("key server down")
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/events", ok[eventInput], guard.Public(), guard.Webhook(verifierFunc(func(context.Context, http.Header, []byte) error { return down })))
	}))
	if status, _, _ := postWebhook(t, s, "/v1/webhooks/events", nil, strings.NewReader(`{"type":"x"}`), 0); status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a verifier that can't decide", status)
	}
}

func TestWebhookInvalidArguments(t *testing.T) {
	v, _ := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
	for name, opt := range map[string]gorbital.RouteOption{
		"nil verifier": guard.Webhook(nil),
		"zero limit":   guard.Webhook(v, guard.WebhookBodyLimit(0)),
	} {
		_, err := tryMount(routes(func(r *gorbital.Router) {
			gorbital.Post(r, "/v1/webhooks/events", ok[eventInput], guard.Public(), opt)
		}))
		if err == nil {
			t.Errorf("%s: Mount() error = nil", name)
		}
	}
}

func TestWebhookOpenAPI(t *testing.T) {
	v, _ := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/events", ok[eventInput], guard.Public(), guard.Webhook(v))
	}))
	op := s.api.OpenAPI().Paths["/v1/webhooks/events"].Post
	for _, status := range []string{"400", "401", "413"} {
		if op.Responses[status] == nil {
			t.Errorf("responses lack %s", status)
		}
	}
	if guards := op.Extensions["x-gorbital-guards"].([]string); strings.Join(guards, ",") != "public,webhook" {
		t.Errorf("x-gorbital-guards = %v", guards)
	}
}

type verifierFunc func(ctx context.Context, header http.Header, body []byte) error

func (f verifierFunc) Verify(ctx context.Context, header http.Header, body []byte) error {
	return f(ctx, header, body)
}

func BenchmarkWebhookGuard(b *testing.B) {
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
	if err != nil {
		b.Fatal(err)
	}
	s, err := tryMount(routes(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/webhooks/raw", func(_ context.Context, in *rawInput) (*receivedOutput, error) {
			return &receivedOutput{}, nil
		}, guard.Public(), guard.Webhook(v))
	}))
	if err != nil {
		b.Fatal(err)
	}
	body := strings.Repeat(`{"type":"payment.succeeded"}`, 40)
	header := signWebhook(webhookKey, "msg_1", time.Now(), body)
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/raw", strings.NewReader(body))
		req.Header = header.Clone()
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		if rec.Code != 200 {
			b.Fatal(rec.Code, rec.Body.String())
		}
	}
}
