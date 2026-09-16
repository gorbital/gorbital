package mail_test

import (
	"strings"
	"testing"

	"gorbital.dev/mail"
)

func valid() mail.Message {
	return mail.Message{
		From:    mail.Address{Name: "My API", Email: "no-reply@example.com"},
		To:      []mail.Address{{Email: "ada@example.com"}},
		Subject: "Verify your email",
		Text:    "Your code is 123456",
	}
}

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*mail.Message)
		wantErr bool
	}{
		{"valid", func(*mail.Message) {}, false},
		{"html only", func(m *mail.Message) { m.Text, m.HTML = "", "<p>hi</p>" }, false},
		{"no recipients", func(m *mail.Message) { m.To = nil }, true},
		{"bad recipient", func(m *mail.Message) { m.To = []mail.Address{{Email: "not-an-email"}} }, true},
		{"bad from", func(m *mail.Message) { m.From.Email = "" }, true},
		{"display name in email field", func(m *mail.Message) { m.To = []mail.Address{{Email: "Ada <ada@example.com>"}} }, true},
		{"header injection in subject", func(m *mail.Message) { m.Subject = "Hi\r\nBcc: victim@example.com" }, true},
		{"header injection in name", func(m *mail.Message) { m.From.Name = "x\nBcc: a@b.c" }, true},
		{"no body", func(m *mail.Message) { m.Text = "" }, true},
		{"bad reply-to", func(m *mail.Message) { m.ReplyTo = []mail.Address{{Email: "nope"}} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid()
			tt.mutate(&m)
			if err := m.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate(%s) = %v, want error %t", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestAddressString(t *testing.T) {
	a := mail.Address{Name: "Ada, Lovelace", Email: "ada@example.com"}
	if got, want := a.String(), `"Ada, Lovelace" <ada@example.com>`; got != want {
		t.Errorf("Address.String() = %q, want %q", got, want)
	}
}

// Provider replies and validation errors become job errors and log lines,
// which must not carry recipients' addresses (security review OPS-5).
func TestRedactAddresses(t *testing.T) {
	tests := []struct{ in, want string }{
		{"550 5.1.1 <jane.doe+tag@example.co.uk>: Recipient address rejected", "550 5.1.1 <[email]>: Recipient address rejected"},
		{"bad recipients ada@example.com, bob_o'neil@mail.example", "bad recipients [email], [email]"},
		{"josé@exämple.de and root@[192.0.2.1]", "[email] and [email]"},
		{"smtp.example.com:587 refused DATA: 554 no", "smtp.example.com:587 refused DATA: 554 no"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := mail.RedactAddresses(tt.in); got != tt.want {
			t.Errorf("RedactAddresses(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	m := valid()
	m.To = []mail.Address{{Email: "not an address jane@example.com"}}
	if err := m.Validate(); err == nil || strings.Contains(err.Error(), "jane@example.com") {
		t.Errorf("Validate() error = %v, want an error without the address", err)
	}
}
