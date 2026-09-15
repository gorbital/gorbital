package orgs

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"gorbital.dev/mail"
)

// Invitation is what an invitation email says.
type Invitation struct {
	// OrgName is the organisation's name and InvitedBy who sent the
	// invitation (a name or email address).
	OrgName   string
	InvitedBy string
	// Role is the role the invited person gets.
	Role string
	// URL opens the invitation in the app's frontend; it carries the token.
	URL string
	// ExpiresIn is how long the invitation stays valid.
	ExpiresIn time.Duration
}

// Emails sends the emails organisations need. Apps use [NewMailEmails] or
// their own templates. Implementations should queue rather than deliver
// inline (jobs.AsyncSender), so requests don't wait for the provider.
type Emails interface {
	// SendInvitation invites to to join an organisation.
	SendInvitation(ctx context.Context, to string, inv Invitation) error
}

type mailEmails struct {
	sender mail.Sender
	app    string
}

// NewMailEmails returns plain, readable emails sent through sender, which
// sets the From address (mail.WithDefaults). appName appears in subjects and
// bodies.
func NewMailEmails(sender mail.Sender, appName string) Emails {
	return mailEmails{sender: sender, app: appName}
}

// oneLine keeps names people chose from breaking a subject line.
var oneLine = strings.NewReplacer("\r", " ", "\n", " ")

func (e mailEmails) SendInvitation(ctx context.Context, to string, inv Invitation) error {
	org := oneLine.Replace(inv.OrgName)
	paragraphs := []string{
		fmt.Sprintf("%s invited you to join %s on %s as %s.", oneLine.Replace(inv.InvitedBy), org, e.app, inv.Role),
		"Accept the invitation: " + inv.URL,
		fmt.Sprintf("It expires in %s. Sign in with this email address to accept it.", days(inv.ExpiresIn)),
		"If you weren't expecting this, you can ignore this email.",
	}
	text, body := "", ""
	for _, p := range paragraphs {
		text += p + "\n\n"
		body += "<p>" + html.EscapeString(p) + "</p>\n"
	}
	return e.sender.Send(ctx, mail.Message{
		To:      []mail.Address{{Email: to}},
		Subject: fmt.Sprintf("Join %s on %s", org, e.app),
		Text:    text,
		HTML:    body,
		Tags:    map[string]string{"category": "orgs_invitation"},
	})
}

// days describes d in whole days, or hours when shorter than a day.
func days(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d == day:
		return "1 day"
	case d > day:
		return fmt.Sprintf("%d days", d/day)
	case d == time.Hour:
		return "1 hour"
	default:
		return fmt.Sprintf("%d hours", max(d/time.Hour, 1))
	}
}
