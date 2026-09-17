package orgs

import (
	"context"
	"fmt"
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

// Emails sends the emails organisations need. Apps use [NewBrandedEmails]
// or their own templates. Implementations should queue rather than deliver
// inline (jobs.AsyncSender), so requests don't wait for the provider.
type Emails interface {
	// SendInvitation invites to to join an organisation.
	SendInvitation(ctx context.Context, to string, inv Invitation) error
}

type mailEmails struct {
	sender mail.Sender
	brand  mail.Brand
}

// NewMailEmails returns the emails of [NewBrandedEmails] with appName as the
// whole brand: no link, logo or support address.
func NewMailEmails(sender mail.Sender, appName string) Emails {
	return NewBrandedEmails(sender, mail.Brand{Name: appName})
}

// NewBrandedEmails returns emails in brand's layout ([mail.Brand.Render])
// sent through sender, which sets the From address (mail.WithDefaults).
// brand.Name appears in subjects and bodies.
func NewBrandedEmails(sender mail.Sender, brand mail.Brand) Emails {
	return mailEmails{sender: sender, brand: brand}
}

// oneLine keeps names people chose from breaking a subject line.
var oneLine = strings.NewReplacer("\r", " ", "\n", " ")

func (e mailEmails) SendInvitation(ctx context.Context, to string, inv Invitation) error {
	org := oneLine.Replace(inv.OrgName)
	app := e.brand.Name
	return e.sender.Send(ctx, e.brand.Message(to, fmt.Sprintf("Join %s on %s", org, app), "orgs_invitation", mail.Email{
		Preheader:  fmt.Sprintf("%s invited you to join %s", oneLine.Replace(inv.InvitedBy), org),
		Title:      fmt.Sprintf("Join %s on %s", org, app),
		Paragraphs: []string{fmt.Sprintf("%s invited you to join %s on %s as %s.", oneLine.Replace(inv.InvitedBy), org, app, inv.Role)},
		Button:     mail.Button{Label: "Accept invitation", URL: inv.URL},
		Closing: []string{
			fmt.Sprintf("The invitation expires in %s. Sign in with this email address to accept it.", days(inv.ExpiresIn)),
			"If you weren't expecting this, you can ignore this email.",
		},
	}))
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
