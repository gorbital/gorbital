package mail_test

import (
	"testing"

	"apistock.dev/mail"
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
