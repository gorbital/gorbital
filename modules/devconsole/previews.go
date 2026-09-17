package devconsole

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"

	"gorbital.dev/httpx"
	gmail "gorbital.dev/mail"
)

// MailPreview is one of the app's emails rendered with sample data, for
// the Dev Portal's template preview (ADR-0074): GET /_dev/mail/previews
// lists them, GET /_dev/mail/preview?name= renders one, and POST
// /_dev/mail/preview/send?name=&to= sends it through the app's mailer to
// the inbox.
type MailPreview struct {
	// Name identifies the preview: letters, digits, dots, hyphens and
	// underscores, such as auth.verification_code.
	Name string `json:"name"`
	// Description says when the app sends it.
	Description string `json:"description"`
	// Category is the message's tag, when the app sets one.
	Category string `json:"category,omitempty"`
}

// MailPreviewList is GET /_dev/mail/previews.
type MailPreviewList struct {
	Previews []MailPreview `json:"previews"`
}

// MailPreviewMessage is a rendered preview: GET /_dev/mail/preview.
type MailPreviewMessage struct {
	MailPreview
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
	// To is the sample recipient the message was rendered for.
	To string `json:"to"`
}

// MailPreviewSent is POST /_dev/mail/preview/send.
type MailPreviewSent struct {
	Name string `json:"name"`
	To   string `json:"to"`
	Sent bool   `json:"sent"`
}

// MailPreviewer renders the app's email previews. Build returns the
// message for to with sample data; the console lists Previews and sends
// what Build returns through Send (the app's mailer, so the message takes
// the same path as a real one).
type MailPreviewer struct {
	Previews []MailPreview
	Build    func(ctx context.Context, name, to string) (gmail.Message, error)
	Send     func(ctx context.Context, m gmail.Message) error
}

// ErrUnknownPreview reports a name Previews doesn't have.
var ErrUnknownPreview = errors.New("no such email preview")

const previewSampleTo = "preview@example.com"

// previewEndpoints are the three preview endpoints; absent without a
// previewer.
func (c *Console) previewEndpoints() []endpoint {
	p := c.sources.MailPreviews
	present := p != nil && p.Build != nil
	find := func(name string) (MailPreview, bool) {
		for _, pv := range p.Previews {
			if pv.Name == name {
				return pv, true
			}
		}
		return MailPreview{}, false
	}
	return []endpoint{
		{path: Prefix + "mail/previews", present: present, serve: func(w http.ResponseWriter, _ *http.Request) error {
			return writeJSON(w, MailPreviewList{Previews: nonNil(p.Previews)})
		}},
		{path: Prefix + "mail/preview", present: present, serve: func(w http.ResponseWriter, r *http.Request) error {
			name := r.URL.Query().Get("name")
			pv, ok := find(name)
			if !ok {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "preview_not_found", "no email preview named "+name))
				return nil
			}
			to := r.URL.Query().Get("to")
			if to == "" {
				to = previewSampleTo
			}
			m, err := p.Build(r.Context(), name, to)
			if err != nil {
				return err
			}
			return writeJSON(w, MailPreviewMessage{MailPreview: pv, Subject: m.Subject, Text: m.Text, HTML: m.HTML, To: to})
		}},
		{path: Prefix + "mail/preview/send", present: present && p.Send != nil, post: true, serve: func(w http.ResponseWriter, r *http.Request) error {
			name := r.URL.Query().Get("name")
			if _, ok := find(name); !ok {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "preview_not_found", "no email preview named "+name))
				return nil
			}
			to := strings.TrimSpace(r.URL.Query().Get("to"))
			if to == "" {
				var body struct {
					To string `json:"to"`
				}
				_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
				to = strings.TrimSpace(body.To)
			}
			if to == "" {
				to = previewSampleTo
			}
			if _, err := mail.ParseAddress(to); err != nil {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusBadRequest, "invalid_address", "to must be an email address"))
				return nil
			}
			m, err := p.Build(r.Context(), name, to)
			if err != nil {
				return err
			}
			if err := p.Send(r.Context(), m); err != nil {
				return err
			}
			return writeJSON(w, MailPreviewSent{Name: name, To: to, Sent: true})
		}},
	}
}
