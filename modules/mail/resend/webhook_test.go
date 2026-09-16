package resend_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/modules/mail/resend"
)

// Test vectors computed outside this package, so the tests don't just
// check the verifier against itself.
var (
	// From Svix's manual verification guide
	// (https://docs.svix.com/receiving/verifying-payloads/how-manual).
	svixSecret    = config.NewSecret("whsec_plJ3nmyCDGBKInavdOK15jsl") // gitleaks:allow (published test vector)
	svixID        = "msg_loFOjxBNrRLzqYUf"
	svixTimestamp = "1731705121"
	svixBody      = `{"event_type":"ping","data":{"success":true}}`
	svixSignature = "v1,rAvfW3dJ/X/qxhsaXPOyyCGmRKsaKWcsNccKXlIktD0="

	// A Resend bounce, signed with Python's hmac and base64 modules:
	// base64(HMAC-SHA256(key, "msg_2mFz5wqS0bL9xRkq7vYt3Hn1Pd.1789552800." + body)).
	bounceSecret    = config.NewSecret("whsec_AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcY") // gitleaks:allow (bytes 1 to 24)
	bounceID        = "msg_2mFz5wqS0bL9xRkq7vYt3Hn1Pd"
	bounceTimestamp = "1789552800"
	bounceBody      = `{"type":"email.bounced","created_at":"2026-09-16T10:00:00.000Z","data":{"email_id":"56761188-7520-42d8-8898-ff6fc54ce618","to":["ada@example.com"],"bounce":{"message":"Mailbox does not exist","subType":"General","type":"Permanent"}}}`
	bounceSignature = "v1,dpmsUXM4UAqHXCHz0MfOEfdY0xJKLhzTiTYmJcArypE="
)

func headers(id, timestamp, signature string) http.Header {
	h := http.Header{}
	h.Set("Svix-Id", id)
	h.Set("Svix-Timestamp", timestamp)
	h.Set("Svix-Signature", signature)
	return h
}

func unix(ts string) time.Time {
	n, _ := strconv.ParseInt(ts, 10, 64)
	return time.Unix(n, 0)
}

func TestVerifyWebhook(t *testing.T) {
	svixNow, bounceNow := unix(svixTimestamp), unix(bounceTimestamp)
	tests := []struct {
		name    string
		secret  config.Secret
		header  http.Header
		body    string
		now     time.Time
		wantErr error
	}{
		{"Svix's published vector", svixSecret, headers(svixID, svixTimestamp, svixSignature), svixBody, svixNow, nil},
		{"a Resend bounce", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow, nil},
		{"secret without its prefix", config.NewSecret("AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcY"), headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow, nil},
		{"several signatures, one valid (secret rotation)", bounceSecret,
			headers(bounceID, bounceTimestamp, "v1,AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= v2,whatever "+bounceSignature+" v1,not-base64"), bounceBody, bounceNow, nil},
		{"4 minutes 59 seconds late", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow.Add(4*time.Minute + 59*time.Second), nil},
		{"4 minutes 59 seconds early", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow.Add(-4*time.Minute - 59*time.Second), nil},

		{"expired", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow.Add(5*time.Minute + time.Second), resend.ErrWebhookTimestamp},
		{"from the future", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow.Add(-5*time.Minute - time.Second), resend.ErrWebhookTimestamp},
		{"body changed", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), strings.Replace(bounceBody, "ada@", "eve@", 1), bounceNow, resend.ErrInvalidWebhook},
		{"body with a trailing newline", bounceSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody + "\n", bounceNow, resend.ErrInvalidWebhook},
		{"another delivery's ID", bounceSecret, headers("msg_other", bounceTimestamp, bounceSignature), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"timestamp changed within the tolerance", bounceSecret, headers(bounceID, "1789552801", bounceSignature), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"another secret", svixSecret, headers(bounceID, bounceTimestamp, bounceSignature), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"signature under another version", bounceSecret, headers(bounceID, bounceTimestamp, "v2"+strings.TrimPrefix(bounceSignature, "v1")), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"signature without a version", bounceSecret, headers(bounceID, bounceTimestamp, strings.TrimPrefix(bounceSignature, "v1,")), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"no signature", bounceSecret, headers(bounceID, bounceTimestamp, ""), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"no ID", bounceSecret, headers("", bounceTimestamp, bounceSignature), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"no timestamp", bounceSecret, headers(bounceID, "", bounceSignature), bounceBody, bounceNow, resend.ErrInvalidWebhook},
		{"timestamp not in seconds", bounceSecret, headers(bounceID, "2026-09-16T10:00:00Z", bounceSignature), bounceBody, bounceNow, resend.ErrInvalidWebhook},
	}
	for _, tt := range tests {
		err := resend.VerifyWebhook(tt.secret, tt.header, []byte(tt.body), tt.now)
		switch {
		case tt.wantErr == nil && err != nil:
			t.Errorf("%s: VerifyWebhook() error = %v", tt.name, err)
		case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
			t.Errorf("%s: VerifyWebhook() error = %v, want %v", tt.name, err, tt.wantErr)
		}
	}
	if !errors.Is(resend.ErrWebhookTimestamp, resend.ErrInvalidWebhook) {
		t.Error("ErrWebhookTimestamp doesn't wrap ErrInvalidWebhook")
	}
}

func TestWebhookSecret(t *testing.T) {
	for _, bad := range []string{"", "whsec_", "whsec_not base64!", "whsec_c2hvcnQ="} {
		err := resend.CheckWebhookSecret(config.NewSecret(bad))
		if err == nil {
			t.Errorf("CheckWebhookSecret(%q) error = nil", bad)
			continue
		}
		if bad != "" && strings.Contains(err.Error(), strings.TrimPrefix(bad, "whsec_")) && strings.TrimPrefix(bad, "whsec_") != "" {
			t.Errorf("CheckWebhookSecret() error quotes the secret: %v", err)
		}
		if resend.VerifyWebhook(config.NewSecret(bad), headers(bounceID, bounceTimestamp, bounceSignature), []byte(bounceBody), unix(bounceTimestamp)) == nil {
			t.Errorf("VerifyWebhook() with secret %q accepted the request", bad)
		}
	}
	if err := resend.CheckWebhookSecret(bounceSecret); err != nil {
		t.Errorf("CheckWebhookSecret() error = %v", err)
	}
}

func TestParseWebhookEvent(t *testing.T) {
	hard, err := resend.ParseWebhookEvent([]byte(bounceBody))
	if err != nil {
		t.Fatalf("ParseWebhookEvent(bounce) error = %v", err)
	}
	if hard.Type != resend.EventEmailBounced || hard.EmailID != "56761188-7520-42d8-8898-ff6fc54ce618" || len(hard.To) != 1 || hard.To[0] != "ada@example.com" ||
		hard.Bounce == nil || !hard.Bounce.Permanent() || hard.Bounce.SubType != "General" || !hard.CreatedAt.Equal(time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("hard bounce = %+v", hard)
	}

	soft, err := resend.ParseWebhookEvent([]byte(`{"type":"email.bounced","created_at":"2026-09-16T10:00:00Z","data":{"to":["ada@example.com"],"bounce":{"type":"Transient","subType":"MailboxFull"}}}`))
	if err != nil || soft.Bounce == nil || soft.Bounce.Permanent() {
		t.Errorf("soft bounce = %+v, %v", soft, err)
	}
	undetermined, err := resend.ParseWebhookEvent([]byte(`{"type":"email.bounced","data":{"to":"ada@example.com","bounce":{"type":"Undetermined"}}}`))
	if err != nil || undetermined.Bounce.Permanent() || len(undetermined.To) != 1 {
		t.Errorf("undetermined bounce = %+v, %v", undetermined, err)
	}

	// Resend's documented example.
	complaint, err := resend.ParseWebhookEvent([]byte(`{"type":"email.complained","created_at":"2026-02-22T23:41:12.126Z","data":{"broadcast_id":"8b146471-e88e-4322-86af-016cd36fd216","created_at":"2026-02-22T23:41:11.894Z","email_id":"56761188-7520-42d8-8898-ff6fc54ce618","message_id":"<111-222-333@email.example.com>","from":"Acme <onboarding@resend.dev>","to":["delivered@resend.dev"],"subject":"Sending this example","template_id":"43f68331-0622-4e15-8202-246a0388854b","tags":{"category":"confirm_email"}}}`))
	if err != nil || complaint.Type != resend.EventEmailComplained || complaint.To[0] != "delivered@resend.dev" || complaint.Bounce != nil {
		t.Errorf("complaint = %+v, %v", complaint, err)
	}
	delivered, err := resend.ParseWebhookEvent([]byte(`{"type":"email.delivered","data":{"to":["ada@example.com"]}}`))
	if err != nil || delivered.Type != resend.EventEmailDelivered {
		t.Errorf("delivered = %+v, %v", delivered, err)
	}
	if other, err := resend.ParseWebhookEvent([]byte(`{"type":"contact.created","data":{}}`)); err != nil || other.Type != "contact.created" {
		t.Errorf("unknown event = %+v, %v; want it accepted", other, err)
	}

	for _, bad := range []string{``, `[]`, `{"data":{}}`, `{"type":"email.bounced","data":{"to":["ada@example.com"]}}`,
		`{"type":"email.complained","data":{"to":[]}}`, `{"type":"email.bounced","data":{"to":[1],"bounce":{"type":"Permanent"}}}`} {
		if _, err := resend.ParseWebhookEvent([]byte(bad)); err == nil {
			t.Errorf("ParseWebhookEvent(%q) error = nil", bad)
		}
	}
}
