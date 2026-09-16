package devconsole

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// mailpitLimit is how many recent messages GET /_dev/mail lists.
	mailpitLimit = 50
	// mailpitTimeout bounds one call to Mailpit's API.
	mailpitTimeout = 5 * time.Second
	// maxMailpitResponse bounds the response read from Mailpit.
	maxMailpitResponse = 4 << 20
)

// Mail is the most recent email captured by Mailpit: GET /_dev/mail.
type Mail struct {
	// WebURL is Mailpit's web interface, where messages can be read.
	WebURL string `json:"web_url"`
	// Total counts every captured message; Messages holds the newest 50.
	Total    int       `json:"total"`
	Messages []Message `json:"messages"`
}

// Message is a captured email's summary.
type Message struct {
	ID          string    `json:"id"`
	From        Address   `json:"from"`
	To          []Address `json:"to"`
	Subject     string    `json:"subject"`
	Snippet     string    `json:"snippet"`
	Created     time.Time `json:"created"`
	Size        int       `json:"size"`
	Attachments int       `json:"attachments"`
	Read        bool      `json:"read"`
}

// Address is an email address with its display name.
type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// MailpitSource returns a [Sources.Mail] reading the newest messages from
// the Mailpit web interface at baseURL, such as http://127.0.0.1:8025, with
// client (http.DefaultClient when nil). An unreachable Mailpit is reported
// with [ErrUnavailable].
func MailpitSource(baseURL string, client *http.Client) (func(ctx context.Context) (Mail, error), error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("devconsole: Mailpit URL %q must be http(s)://host:port", baseURL)
	}
	if client == nil {
		client = http.DefaultClient
	}
	base := strings.TrimRight(u.String(), "/")
	return func(ctx context.Context) (Mail, error) {
		ctx, cancel := context.WithTimeout(ctx, mailpitTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/messages?limit=%d", base, mailpitLimit), nil)
		if err != nil {
			return Mail{}, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return Mail{}, fmt.Errorf("%w: Mailpit didn't answer at %s: %w", ErrUnavailable, base, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Mail{}, fmt.Errorf("%w: Mailpit at %s answered %s", ErrUnavailable, base, resp.Status)
		}
		var list struct {
			Total    int `json:"total"`
			Messages []struct {
				ID          string           `json:"ID"`
				From        *mailpitAddress  `json:"From"`
				To          []mailpitAddress `json:"To"`
				Subject     string           `json:"Subject"`
				Snippet     string           `json:"Snippet"`
				Created     time.Time        `json:"Created"`
				Size        int              `json:"Size"`
				Attachments int              `json:"Attachments"`
				Read        bool             `json:"Read"`
			} `json:"messages"`
		}
		dec := json.NewDecoder(io.LimitReader(resp.Body, maxMailpitResponse))
		if err := dec.Decode(&list); err != nil {
			return Mail{}, errors.Join(ErrUnavailable, fmt.Errorf("read Mailpit's messages: %w", err))
		}
		mail := Mail{WebURL: base, Total: list.Total, Messages: make([]Message, 0, len(list.Messages))}
		for _, m := range list.Messages {
			msg := Message{
				ID: m.ID, Subject: m.Subject, Snippet: m.Snippet, Created: m.Created,
				Size: m.Size, Attachments: m.Attachments, Read: m.Read, To: make([]Address, 0, len(m.To)),
			}
			if m.From != nil {
				msg.From = Address(*m.From)
			}
			for _, to := range m.To {
				msg.To = append(msg.To, Address(to))
			}
			mail.Messages = append(mail.Messages, msg)
		}
		return mail, nil
	}, nil
}

type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}
