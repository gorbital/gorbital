package resend_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/mail"
	"gorbital.dev/modules/mail/resend"
)

const apiKey = "re_test_0123456789" // gitleaks:allow (placeholder for the fake server)

type request struct {
	method, path, auth, idempotency, contentType string
	body                                         map[string]any
}

// fakeAPI records requests and answers with status and body.
type fakeAPI struct {
	mu       sync.Mutex
	requests []request
	status   int
	body     string
}

func startAPI(t *testing.T, status int, body string) (*fakeAPI, *resend.Sender) {
	t.Helper()
	api := &fakeAPI{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := request{
			method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
			idempotency: r.Header.Get("Idempotency-Key"), contentType: r.Header.Get("Content-Type"),
		}
		_ = json.Unmarshal(raw, &req.body)
		api.mu.Lock()
		api.requests = append(api.requests, req)
		api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(api.status)
		_, _ = io.WriteString(w, api.body)
	}))
	t.Cleanup(srv.Close)
	s, err := resend.New(config.NewSecret(apiKey), resend.WithBaseURL(srv.URL+"/"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return api, s
}

func message() mail.Message {
	return mail.Message{
		From:           mail.Address{Name: "Acme", Email: "no-reply@acme.test"},
		To:             []mail.Address{{Name: "Ada Lovelace", Email: "ada@example.com"}},
		ReplyTo:        []mail.Address{{Email: "support@acme.test"}},
		Subject:        "Your code",
		Text:           "Your code is 123456.",
		HTML:           "<p>Your code is 123456.</p>",
		IdempotencyKey: "job-42",
		Tags:           map[string]string{"category": "verification", "app": "acme-api"},
	}
}

func TestSendPostsTheMessage(t *testing.T) {
	api, s := startAPI(t, http.StatusOK, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
	if err := s.Send(context.Background(), message()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(api.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(api.requests))
	}
	got := api.requests[0]
	if got.method != http.MethodPost || got.path != "/emails" || got.auth != "Bearer "+apiKey ||
		got.idempotency != "job-42" || got.contentType != "application/json" {
		t.Errorf("request = %+v", got)
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(got.body)
	body := strings.TrimSpace(buf.String())
	want := `{"from":"\"Acme\" <no-reply@acme.test>","html":"<p>Your code is 123456.</p>","reply_to":["<support@acme.test>"],` +
		`"subject":"Your code","tags":[{"name":"app","value":"acme-api"},{"name":"category","value":"verification"}],"text":"Your code is 123456.",` +
		`"to":["\"Ada Lovelace\" <ada@example.com>"]}`
	if body != want {
		t.Errorf("body = %s\nwant   %s", body, want)
	}
}

func TestSendLongIdempotencyKeyIsHashed(t *testing.T) {
	api, s := startAPI(t, http.StatusOK, `{"id":"1"}`)
	m := message()
	m.IdempotencyKey = strings.Repeat("k", 300)
	if err := s.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if key := api.requests[0].idempotency; !strings.HasPrefix(key, "sha256-") || len(key) > 256 {
		t.Errorf("Idempotency-Key = %q, want a hash within 256 characters", key)
	}
}

func TestSendClassifiesErrors(t *testing.T) {
	tests := []struct {
		status       int
		body         string
		wantRejected bool
		wantText     string
	}{
		{422, `{"statusCode":422,"name":"validation_error","message":"Invalid to field."}`, true, "validation_error: Invalid to field."},
		{403, `{"statusCode":403,"name":"validation_error","message":"The acme.test domain is not verified."}`, true, "mail.from_email"},
		{409, `{"statusCode":409,"name":"invalid_idempotent_request","message":"Same key, different payload."}`, true, "invalid_idempotent_request"},
		{409, `{"statusCode":409,"name":"concurrent_idempotent_requests","message":"In progress."}`, false, "concurrent_idempotent_requests"},
		{401, `{"statusCode":401,"name":"missing_api_key","message":"Missing API key."}`, false, "RESEND_API_KEY"},
		{429, `{"statusCode":429,"name":"rate_limit_exceeded","message":"Too many requests."}`, false, "rate_limit_exceeded"},
		{500, `not json`, false, "Internal Server Error"},
	}
	for _, tt := range tests {
		_, s := startAPI(t, tt.status, tt.body)
		err := s.Send(context.Background(), message())
		if err == nil || errors.Is(err, mail.ErrRejected) != tt.wantRejected || !strings.Contains(err.Error(), tt.wantText) {
			t.Errorf("status %d: Send() error = %v, want rejected %t containing %q", tt.status, err, tt.wantRejected, tt.wantText)
		}
		if err != nil && strings.Contains(err.Error(), apiKey) {
			t.Errorf("status %d: error reveals the API key: %v", tt.status, err)
		}
	}
}

func TestSendRejectsBeforeCallingTheAPI(t *testing.T) {
	api, s := startAPI(t, http.StatusOK, `{}`)
	invalid := message()
	invalid.Subject = ""
	badTag := message()
	badTag.Tags = map[string]string{"user email": "ada@example.com"}
	for name, m := range map[string]mail.Message{"invalid message": invalid, "bad tag": badTag} {
		if err := s.Send(context.Background(), m); !errors.Is(err, mail.ErrRejected) {
			t.Errorf("%s: Send() error = %v, want ErrRejected", name, err)
		}
	}
	if len(api.requests) != 0 {
		t.Errorf("API called %d times, want 0", len(api.requests))
	}
}

func TestSendRespectsCancellation(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	s, err := resend.New(config.NewSecret(apiKey), resend.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := s.Send(ctx, message()); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, mail.ErrRejected) {
		t.Errorf("Send() error = %v, want a temporary deadline error", err)
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := resend.New(config.Secret{}); err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY") {
		t.Errorf("New(no key) error = %v, want a RESEND_API_KEY hint", err)
	}
	if _, err := resend.New(config.NewSecret("re_ 123")); err == nil {
		t.Error("New(key with a space) error = nil")
	}
	if _, err := resend.New(config.NewSecret(apiKey), resend.WithBaseURL("api.resend.com")); err == nil {
		t.Error("New(relative base URL) error = nil")
	}
}
