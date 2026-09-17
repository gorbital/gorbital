package mail_test

import (
	"strings"
	"testing"

	"gorbital.dev/mail"
)

func TestBrandRender(t *testing.T) {
	b := mail.Brand{Name: "Acme", URL: "https://acme.example", SupportEmail: "help@acme.example", Footer: "Acme Ltd, 1 Orbit Way"}
	e := mail.Email{
		Preheader:  "Your Acme verification code is 483920",
		Title:      "Verify your email",
		Paragraphs: []string{"Enter this code to confirm your email address for Acme."},
		Code:       "483920", CodeLabel: "Verification code", CodeNote: "It expires in 15 minutes.",
		Button:  mail.Button{Label: "Open Acme", URL: "https://acme.example/verify?x=1&y=2"},
		Closing: []string{"If you didn't create an account, you can ignore this email."},
	}
	text, html := b.Render(e)

	for _, want := range []string{
		"Verify your email\n\n",
		"Enter this code to confirm your email address for Acme.\n\n",
		"Verification code: 483920\nIt expires in 15 minutes.\n\n",
		"Open Acme: https://acme.example/verify?x=1&y=2\n\n",
		"If you didn't create an account, you can ignore this email.\n\n",
		"-- \nAcme · https://acme.example\nQuestions? Write to help@acme.example\nAcme Ltd, 1 Orbit Way\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	for _, want := range []string{
		"<!DOCTYPE html>",
		`<meta name="color-scheme" content="light">`,
		"Your Acme verification code is 483920</div>",
		`<a href="https://acme.example" style="color:#0b0c0a;text-decoration:none;">Acme</a>`,
		"<h1", ">Verify your email</h1>",
		">Enter this code to confirm your email address for Acme.</p>",
		">Verification code</div>",
		">483920</div>",
		">It expires in 15 minutes.</p>",
		`<a href="https://acme.example/verify?x=1&amp;y=2" style="display:inline-block;`,
		">Open Acme</a>",
		"Or paste this link into your browser:",
		">If you didn&#39;t create an account, you can ignore this email.</p>",
		`<a href="mailto:help@acme.example"`,
		"Acme Ltd, 1 Orbit Way",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html lacks %q:\n%s", want, html)
		}
	}
}

func TestBrandRenderEscapesAndOmits(t *testing.T) {
	b := mail.Brand{Name: "Acme"}
	text, html := b.Render(mail.Email{Paragraphs: []string{"<b>Runners</b> & co"}})
	if strings.Contains(html, "<b>Runners</b>") || !strings.Contains(html, "&lt;b&gt;Runners&lt;/b&gt; &amp; co") {
		t.Errorf("html = %s; want escaped paragraph", html)
	}
	for _, absent := range []string{"<h1", "Verification", "Or paste", "mailto:", "border-top"} {
		if strings.Contains(html, absent) {
			t.Errorf("html has %q for an email without that part", absent)
		}
	}
	if want := "<b>Runners</b> & co\n\n-- \nAcme\n"; text != want {
		t.Errorf("text = %q; want %q", text, want)
	}
	if _, html := (mail.Brand{}).Render(mail.Email{Button: mail.Button{URL: "javascript:alert(1)"}}); strings.Contains(html, "javascript:") {
		t.Errorf("html keeps an unsafe button URL: %s", html)
	}
}

func TestBrandLogo(t *testing.T) {
	_, html := (mail.Brand{Name: "Acme", LogoURL: "https://acme.example/logo.png"}).Render(mail.Email{Title: "Hi"})
	if !strings.Contains(html, `<img src="https://acme.example/logo.png" alt="Acme" height="32"`) || strings.Contains(html, "border-radius:7px") {
		t.Errorf("html = %s; want the logo instead of the wordmark", html)
	}
}

func TestBrandMessage(t *testing.T) {
	m := mail.Brand{Name: "Acme"}.Message("ada@example.com", "Hello", "greeting", mail.Email{Title: "Hello Ada"})
	if len(m.To) != 1 || m.To[0].Email != "ada@example.com" || m.Subject != "Hello" || m.Tags["category"] != "greeting" ||
		!strings.Contains(m.Text, "Hello Ada") || !strings.Contains(m.HTML, ">Hello Ada</h1>") || m.From.Email != "" {
		t.Errorf("message = %+v", m)
	}
	if m := (mail.Brand{}).Message("ada@example.com", "Hello", "", mail.Email{}); m.Tags != nil {
		t.Errorf("tags = %v without a category", m.Tags)
	}
}
