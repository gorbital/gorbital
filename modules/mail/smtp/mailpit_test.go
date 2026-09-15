package smtp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"gorbital.dev/mail"
	"gorbital.dev/modules/mail/smtp"
)

// Mailpit in Docker (compose.yaml). The test is skipped when these are
// unset, and fails when GORBITAL_REQUIRE_MAILPIT=1, as in CI.
const (
	envMailpitSMTP = "GORBITAL_TEST_MAILPIT_SMTP"
	envMailpitURL  = "GORBITAL_TEST_MAILPIT_URL"
)

func TestSendToMailpit(t *testing.T) {
	addr, api := os.Getenv(envMailpitSMTP), os.Getenv(envMailpitURL)
	if addr == "" || api == "" {
		msg := "Mailpit is not configured: run `docker compose up -d --wait` and set " + envMailpitSMTP + " and " + envMailpitURL
		if os.Getenv("GORBITAL_REQUIRE_MAILPIT") == "1" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	subject := "gorbital smtp test " + time.Now().Format(time.RFC3339Nano)
	s, err := smtp.New(addr, smtp.WithTLS(smtp.TLSNone))
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), mail.Message{
		From:    mail.Address{Name: "gorbital", Email: "no-reply@gorbital.test"},
		To:      []mail.Address{{Email: "ada@example.com"}},
		Subject: subject,
		Text:    "Hello from the smtp module.",
		HTML:    "<p>Hello from the smtp module.</p>",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	search := api + "/api/v1/search?query=" + url.QueryEscape(`subject:"`+subject+`"`)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var result struct {
			Messages []struct {
				Subject string `json:"Subject"`
			} `json:"messages"`
		}
		resp, err := http.Get(search) //nolint:gosec,noctx // test URL from the environment
		if err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&result)
			_ = resp.Body.Close()
		}
		if len(result.Messages) == 1 && result.Messages[0].Subject == subject {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Mailpit has no message with subject %q (last error: %v)", subject, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
