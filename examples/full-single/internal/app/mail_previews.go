package app

import (
	"context"

	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/devconsole"
)

// mailPreviews renders the app's emails with sample data for the Dev
// Portal's template preview (ADR-0074): the auth module's messages and a
// plain test message. Add your own emails here as you write them. Sending
// goes through the app's mailer, so a preview lands in the inbox the way
// a real message does.
func (a *App) mailPreviews() *devconsole.MailPreviewer {
	kinds := map[string]func(ctx context.Context, to string) (mail.Message, error){}
	var previews []devconsole.MailPreview
	for _, p := range authlib.EmailPreviews(ServiceName) {
		kinds[p.Name] = p.Build
		previews = append(previews, devconsole.MailPreview{Name: p.Name, Description: p.Description, Category: p.Category})
	}
	previews = append(previews, devconsole.MailPreview{Name: "test", Description: "A plain message, to check delivery", Category: "test"})
	kinds["test"] = func(_ context.Context, to string) (mail.Message, error) {
		return mail.Message{To: []mail.Address{{Email: to}}, Subject: "Test email from " + ServiceName,
			Text: "This is a test message from " + ServiceName + ". If you can read it, email delivery works.",
			HTML: "<p>This is a test message from " + ServiceName + ". If you can read it, email delivery works.</p>",
			Tags: map[string]string{"category": "test"}}, nil
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
