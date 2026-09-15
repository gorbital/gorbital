package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/modules/openapi"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// MailStatusResponse is how the app sends email.
type MailStatusResponse struct {
	Provider  string            `json:"provider" enum:"resend,smtp" doc:"Chosen with aps add mail"`
	Delivery  string            `json:"delivery" enum:"mailpit,provider" doc:"mailpit: every email goes to the development inbox; provider: real email"`
	Details   map[string]string `json:"details" doc:"Provider configuration from the environment, without secrets"`
	FromName  string            `json:"from_name" doc:"Runtime setting mail.from_name" example:"Acme"`
	FromEmail string            `json:"from_email" doc:"Runtime setting mail.from_email" example:"no-reply@acme.com"`
	ReplyTo   string            `json:"reply_to,omitempty" doc:"Runtime setting mail.reply_to"`
}

// TestEmailResponse confirms a queued test email.
type TestEmailResponse struct {
	Status   string `json:"status" enum:"queued"`
	To       string `json:"to"`
	Delivery string `json:"delivery" enum:"mailpit,provider"`
}

type mailStatusOutput struct{ Body MailStatusResponse }

type testEmailOutput struct{ Body TestEmailResponse }

type testEmailInput struct {
	Body struct {
		_  struct{} `json:"-" additionalProperties:"true"`
		To string   `json:"to" format:"email" maxLength:"254" example:"you@example.com" doc:"Who receives the test email"`
	}
}

type mailHandler struct {
	svc *opsusecase.Service
}

// RegisterMail adds the email operations to api.
func RegisterMail(api huma.API, svc *opsusecase.Service) {
	h := &mailHandler{svc: svc}
	tags := []string{"Ops: email"}

	huma.Register(api, huma.Operation{
		OperationID: "ops-get-mail", Method: http.MethodGet, Path: "/ops/mail",
		Summary:     "Show how the app sends email",
		Description: "The provider, whether email goes to Mailpit or the provider, and the current sender. Change the sender with `PUT /ops/settings/mail.from_email`.",
		Tags:        tags, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, h.status)
	huma.Register(api, huma.Operation{
		OperationID: "ops-send-test-email", Method: http.MethodPost, Path: "/ops/mail/test",
		Summary:     "Send a test email",
		Description: "Queues a test email through the same path as every other email. Check its delivery in `GET /ops/jobs/runs?kind=apistock.mail.send`.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, h.sendTest)
}

func (h *mailHandler) status(ctx context.Context, _ *struct{}) (*mailStatusOutput, error) {
	st, err := h.svc.MailStatus(ctx)
	if err != nil {
		return nil, err
	}
	details := st.Details
	if details == nil {
		details = map[string]string{}
	}
	return &mailStatusOutput{Body: MailStatusResponse{
		Provider: st.Provider, Delivery: st.Delivery, Details: details,
		FromName: st.FromName, FromEmail: st.FromEmail, ReplyTo: st.ReplyTo,
	}}, nil
}

func (h *mailHandler) sendTest(ctx context.Context, in *testEmailInput) (*testEmailOutput, error) {
	if err := h.svc.SendTestEmail(ctx, in.Body.To); err != nil {
		return nil, err
	}
	st, err := h.svc.MailStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &testEmailOutput{Body: TestEmailResponse{Status: "queued", To: in.Body.To, Delivery: st.Delivery}}, nil
}
