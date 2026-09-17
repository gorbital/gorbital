package app

import (
	"context"
	"time"

	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/devconsole"
	orgslib "gorbital.dev/modules/orgs"
)

// mailPreviews renders the app's emails with sample data for the Dev
// Portal's template preview (ADR-0074): the auth module's messages, the
// organisation invitation and a plain test message. Add your own emails here as you write them. Sending
// goes through the app's mailer, so a preview lands in the inbox the way
// a real message does.
func (a *App) mailPreviews() *devconsole.MailPreviewer {
	kinds := map[string]func(ctx context.Context, to string) (mail.Message, error){}
	var previews []devconsole.MailPreview
	for _, p := range authlib.BrandedEmailPreviews(a.brand()) {
		kinds[p.Name] = p.Build
		previews = append(previews, devconsole.MailPreview{Name: p.Name, Description: p.Description, Category: p.Category})
	}
	previews = append(previews, devconsole.MailPreview{Name: "orgs.invitation", Description: "When someone is invited to an organisation", Category: "orgs_invitation"})
	kinds["orgs.invitation"] = func(ctx context.Context, to string) (mail.Message, error) {
		var captured mail.Message
		emails := orgslib.NewBrandedEmails(mail.SenderFunc(func(_ context.Context, m mail.Message) error {
			captured = m
			return nil
		}), a.brand())
		err := emails.SendInvitation(ctx, to, orgslib.Invitation{
			OrgName: "Acme", InvitedBy: "ada@example.com", Role: "member",
			URL: a.cfg.Social.PublicURL + "/invitations/preview", ExpiresIn: 7 * 24 * time.Hour,
		})
		return captured, err
	}
	previews = append(previews, devconsole.MailPreview{Name: "test", Description: "A plain message, to check delivery", Category: "test"})
	kinds["test"] = func(_ context.Context, to string) (mail.Message, error) {
		return a.brand().Message(to, "Test email from "+ServiceName, "test", mail.Email{
			Preheader:  "If you can read it, email delivery works",
			Title:      "Email delivery works",
			Paragraphs: []string{"This is a test message from " + ServiceName + ". If you can read it, email delivery works."},
		}), nil
	}
	return &devconsole.MailPreviewer{
		Previews: previews,
		Build: func(ctx context.Context, name, to string) (mail.Message, error) {
			build, ok := kinds[name]
			if !ok {
				return mail.Message{}, devconsole.ErrUnknownPreview
			}
			return build(ctx, to)
		},
		Send: func(ctx context.Context, m mail.Message) error { return a.mailer.Send(ctx, m) },
	}
}
