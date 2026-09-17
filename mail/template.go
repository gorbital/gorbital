package mail

import (
	"bytes"
	htmltemplate "html/template"
	"net/url"
	"strings"
)

// Brand is what every email an app sends has in common: the name at the top
// and in the footer, where it links, an optional logo, and how to reach
// support. Apps build one in internal/app and pass it to the modules'
// NewBrandedEmails; their own emails go through [Brand.Message] (ADR-0078).
type Brand struct {
	// Name is the product's name, shown as the wordmark when LogoURL is empty
	// and always in the footer.
	Name string
	// URL is where the name links and the address in the footer; empty for
	// neither.
	URL string
	// LogoURL is the absolute URL of a logo shown in place of the wordmark,
	// up to 32px high. Empty shows the name.
	LogoURL string
	// SupportEmail ends the footer with where to write; empty omits it.
	SupportEmail string
	// Footer is one more line for the footer, such as a postal address or
	// why the recipient gets the email. Empty omits it.
	Footer string
}

// An Email is the content of one transactional message, rendered by
// [Brand.Render] as HTML and plain text. Every field is optional; the parts
// appear in the order they are declared.
type Email struct {
	// Preheader is the summary inbox lists show after the subject. It is not
	// shown in the opened email.
	Preheader string
	// Title is the heading.
	Title string
	// Paragraphs are the message, before the code or button.
	Paragraphs []string
	// Code is a one-time code, shown large in its own block. CodeLabel is
	// its caption ("Verification code") and CodeNote the line under the
	// block ("It expires in 15 minutes.").
	Code      string
	CodeLabel string
	CodeNote  string
	// Button is the main action; the zero value shows none.
	Button Button
	// Closing paragraphs come last, set smaller: "If you didn't do this…".
	Closing []string
}

// A Button is a call to action: the label on the button and the address it
// opens, which is repeated as a link for clients that don't show buttons.
type Button struct {
	Label string
	URL   string
}

// Message returns a message to to with subject and the category tag, with e
// rendered by b. From is left for the sender's defaults.
func (b Brand) Message(to, subject, category string, e Email) Message {
	text, html := b.Render(e)
	m := Message{
		To:      []Address{{Email: to}},
		Subject: subject,
		Text:    text,
		HTML:    html,
	}
	if category != "" {
		m.Tags = map[string]string{"category": category}
	}
	return m
}

// Render returns e as plain text and as HTML in b's layout: a wordmark, one
// card with the message, a footer. The HTML is one 560px column of tables
// with inline styles, so it reads the same in Gmail, Outlook and Apple
// Mail, in light colours whatever the client's theme.
func (b Brand) Render(e Email) (text, html string) {
	// Only web and mail links are rendered; anything else (javascript:,
	// data:) is dropped rather than shown as text.
	if !safeURL(b.URL) {
		b.URL = ""
	}
	if !safeURL(b.LogoURL) {
		b.LogoURL = ""
	}
	if !safeURL(e.Button.URL) {
		e.Button = Button{}
	}
	return b.text(e), b.html(e)
}

// safeURL reports whether u is empty or an http, https or mailto URL.
func safeURL(u string) bool {
	if u == "" {
		return true
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

func (b Brand) text(e Email) string {
	var sb strings.Builder
	part := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			sb.WriteString(s)
			sb.WriteString("\n\n")
		}
	}
	part(e.Title)
	for _, p := range e.Paragraphs {
		part(p)
	}
	if e.Code != "" {
		label := e.CodeLabel
		if label == "" {
			label = "Code"
		}
		code := label + ": " + e.Code
		if e.CodeNote != "" {
			code += "\n" + e.CodeNote
		}
		part(code)
	}
	if e.Button.URL != "" {
		label := e.Button.Label
		if label == "" {
			label = "Open"
		}
		part(label + ": " + e.Button.URL)
	}
	for _, p := range e.Closing {
		part(p)
	}
	var footer []string
	if b.Name != "" {
		footer = append(footer, b.Name)
	}
	if b.URL != "" {
		footer = append(footer, b.URL)
	}
	if len(footer) > 0 || b.SupportEmail != "" || b.Footer != "" {
		sb.WriteString("-- \n")
		if len(footer) > 0 {
			sb.WriteString(strings.Join(footer, " · ") + "\n")
		}
		if b.SupportEmail != "" {
			sb.WriteString("Questions? Write to " + b.SupportEmail + "\n")
		}
		if b.Footer != "" {
			sb.WriteString(b.Footer + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func (b Brand) html(e Email) string {
	var buf bytes.Buffer
	if err := layout.Execute(&buf, layoutData{Brand: b, Email: e}); err != nil {
		// The template is a constant and the data plain strings; a failure
		// here is a programming error, and an empty HTML body still sends
		// (the text body is the alternative).
		return ""
	}
	return buf.String()
}

type layoutData struct {
	Brand Brand
	Email Email
}

// Colours from the brand's light palette (docs/brand/theme.md): paper
// ground, white card, ink type, the accent as a fill only. The code block
// is the one dark surface, so the code reads as something to copy.
const layoutHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<meta name="supported-color-schemes" content="light">
<meta name="x-apple-disable-message-reformatting">
<title>{{if .Email.Title}}{{.Email.Title}}{{else}}{{.Brand.Name}}{{end}}</title>
<style>
body { margin: 0; padding: 0; background: #f2f1ec; -webkit-text-size-adjust: 100%; }
table { border-collapse: collapse; }
a { color: #5c7a12; }
@media only screen and (max-width: 620px) {
  .column { width: 100% !important; }
  .card { padding: 28px 22px !important; }
  .code { font-size: 26px !important; letter-spacing: 6px !important; }
}
</style>
</head>
<body style="margin:0;padding:0;background:#f2f1ec;">
{{if .Email.Preheader}}<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;mso-hide:all;">{{.Email.Preheader}}</div>{{end}}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#f2f1ec;">
<tr><td align="center" style="padding:32px 16px;">
<table role="presentation" class="column" width="560" cellpadding="0" cellspacing="0" border="0" style="width:560px;max-width:100%;">
<tr><td style="padding:0 6px 18px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
{{if .Brand.LogoURL}}<td>{{if .Brand.URL}}<a href="{{.Brand.URL}}" style="text-decoration:none;">{{end}}<img src="{{.Brand.LogoURL}}" alt="{{.Brand.Name}}" height="32" style="display:block;height:32px;border:0;">{{if .Brand.URL}}</a>{{end}}</td>
{{else}}<td width="14" style="width:14px;height:14px;background:#c6f24a;border-radius:7px;font-size:0;line-height:0;">&nbsp;</td>
<td style="padding-left:10px;font-family:Manrope,-apple-system,'Segoe UI',Helvetica,Arial,sans-serif;font-size:17px;font-weight:700;line-height:1;letter-spacing:-0.02em;color:#0b0c0a;">{{if .Brand.URL}}<a href="{{.Brand.URL}}" style="color:#0b0c0a;text-decoration:none;">{{.Brand.Name}}</a>{{else}}{{.Brand.Name}}{{end}}</td>{{end}}
</tr></table>
</td></tr>
<tr><td class="card" style="background:#ffffff;border:1px solid #dcdbd2;border-radius:14px;padding:36px 40px;font-family:Manrope,-apple-system,'Segoe UI',Helvetica,Arial,sans-serif;color:#14140f;">
{{if .Email.Title}}<h1 style="margin:0 0 16px;font-size:22px;font-weight:700;line-height:1.3;letter-spacing:-0.01em;color:#0b0c0a;">{{.Email.Title}}</h1>{{end}}
{{range .Email.Paragraphs}}<p style="margin:0 0 14px;font-size:15px;line-height:1.6;color:#14140f;">{{.}}</p>
{{end}}{{if .Email.Code}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:22px 0 10px;"><tr>
<td align="center" style="background:#0b0c0a;border-radius:12px;padding:22px 24px;">
{{if .Email.CodeLabel}}<div style="font-family:'Geist Mono',SFMono-Regular,Menlo,Consolas,monospace;font-size:11px;font-weight:600;line-height:1;letter-spacing:0.14em;text-transform:uppercase;color:#c6f24a;margin-bottom:12px;">{{.Email.CodeLabel}}</div>{{end}}
<div class="code" style="font-family:'Geist Mono',SFMono-Regular,Menlo,Consolas,monospace;font-size:32px;font-weight:700;line-height:1.2;letter-spacing:8px;color:#f2f1ec;padding-left:8px;">{{.Email.Code}}</div>
</td></tr></table>
{{if .Email.CodeNote}}<p style="margin:0 0 14px;font-size:13px;line-height:1.6;color:#57564f;text-align:center;">{{.Email.CodeNote}}</p>{{end}}
{{end}}{{if .Email.Button.URL}}<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:22px 0 8px;"><tr>
<td style="background:#c6f24a;border-radius:10px;"><a href="{{.Email.Button.URL}}" style="display:inline-block;padding:13px 22px;font-family:Manrope,-apple-system,'Segoe UI',Helvetica,Arial,sans-serif;font-size:14px;font-weight:700;line-height:1;color:#0b0c0a;text-decoration:none;">{{if .Email.Button.Label}}{{.Email.Button.Label}}{{else}}Open{{end}}</a></td>
</tr></table>
<p style="margin:0 0 14px;font-size:12px;line-height:1.6;color:#75756c;word-break:break-all;">Or paste this link into your browser: <a href="{{.Email.Button.URL}}" style="color:#5c7a12;">{{.Email.Button.URL}}</a></p>
{{end}}{{if .Email.Closing}}<div style="border-top:1px solid #e9e8e1;margin-top:20px;padding-top:16px;">
{{range .Email.Closing}}<p style="margin:0 0 10px;font-size:13px;line-height:1.6;color:#57564f;">{{.}}</p>
{{end}}</div>{{end}}
</td></tr>
<tr><td style="padding:20px 6px 0;font-family:Manrope,-apple-system,'Segoe UI',Helvetica,Arial,sans-serif;font-size:12px;line-height:1.7;color:#75756c;">
{{if .Brand.Name}}{{.Brand.Name}}{{end}}{{if .Brand.URL}}{{if .Brand.Name}} &middot; {{end}}<a href="{{.Brand.URL}}" style="color:#75756c;">{{.Brand.URL}}</a>{{end}}{{if .Brand.SupportEmail}}<br>Questions? Write to <a href="mailto:{{.Brand.SupportEmail}}" style="color:#75756c;">{{.Brand.SupportEmail}}</a>{{end}}{{if .Brand.Footer}}<br>{{.Brand.Footer}}{{end}}
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`

var layout = htmltemplate.Must(htmltemplate.New("email").Parse(layoutHTML))
