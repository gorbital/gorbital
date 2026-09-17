package delivery

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/openapi"

	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// MailStatusResponse is how the app sends email.
type MailStatusResponse struct {
	Provider  string            `json:"provider" enum:"resend,smtp" doc:"Chosen with orb add mail"`
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

// SuppressionResponse is an address on the suppression list.
type SuppressionResponse struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email" doc:"Normalized: trimmed and in lower case" example:"ada@example.com"`
	Reason    string    `json:"reason" enum:"bounce,complaint" doc:"bounce: a permanent bounce; complaint: marked as spam"`
	Source    string    `json:"source" doc:"Who reported it" example:"resend"`
	Detail    string    `json:"detail,omitempty" doc:"The provider's classification" example:"Permanent/General"`
	CreatedAt time.Time `json:"created_at" doc:"When the address was first suppressed"`
	UpdatedAt time.Time `json:"updated_at" doc:"The latest bounce or complaint"`
}

// SuppressionPage is a page of suppressions, most recently added first.
type SuppressionPage struct {
	Suppressions []SuppressionResponse `json:"suppressions"`
	NextCursor   string                `json:"next_cursor,omitempty"`
}

type mailStatusOutput struct{ Body MailStatusResponse }

type suppressionPageOutput struct{ Body SuppressionPage }

type suppressionOutput struct{ Body SuppressionResponse }

type listSuppressionsInput struct {
	Reason string `query:"reason" enum:"bounce,complaint" doc:"Only suppressions with this reason"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Cursor string `query:"cursor" maxLength:"30" doc:"next_cursor from the previous page"`
}

type removeSuppressionInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		_      struct{} `json:"-" additionalProperties:"true"`
		Reason string   `json:"reason" maxLength:"500" doc:"Why the address may receive email again; required. Don't include the address" example:"The mailbox exists again (support ticket 4821)"`
	}
}

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
func RegisterMail(r *gorbital.Router, svc *opsusecase.Service) {
	h := &mailHandler{svc: svc}
	tags := []string{"Ops: email"}

	operation.Register(r, huma.Operation{
		OperationID: "ops-get-mail", Method: http.MethodGet, Path: "/ops/mail",
		Summary:     "Show how the app sends email",
		Description: "The provider, whether email goes to Mailpit or the provider, and the current sender. Change the sender with `PUT /ops/settings/mail.from_email`.",
		Tags:        tags, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, h.status)
	operation.Register(r, huma.Operation{
		OperationID: "ops-send-test-email", Method: http.MethodPost, Path: "/ops/mail/test",
		Summary:     "Send a test email",
		Description: "Queues a test email through the same path as every other email. Check its delivery in `GET /ops/jobs/runs?kind=gorbital.mail.send`. Each operator can send 5 an hour.",
		Tags:        tags, Security: openapi.Bearer, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}, h.sendTest)
	operation.Register(r, huma.Operation{
		OperationID: "ops-list-mail-suppressions", Method: http.MethodGet, Path: "/ops/mail/suppressions",
		Summary: "List suppressed addresses",
		Description: "Addresses that bounced permanently or marked an email as spam, most recently added first. The app sends them no email: " +
			"such jobs are cancelled. The provider's webhook adds them (`POST /v1/webhooks/resend`).",
		Tags: tags, Security: openapi.Bearer,
		Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden},
	}, h.listSuppressions)
	operation.Register(r, huma.Operation{
		OperationID: "ops-remove-mail-suppression", Method: http.MethodDelete, Path: "/ops/mail/suppressions/{id}",
		Summary: "Remove a suppressed address",
		Description: "The address receives email again until its next permanent bounce or complaint. A reason is required and recorded in the audit " +
			"event `mail.suppression.removed`. Returns the removed suppression.",
		Tags: tags, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.removeSuppression)
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
	delivery, err := h.svc.SendTestEmail(ctx, in.Body.To)
	if err != nil {
		return nil, err
	}
	return &testEmailOutput{Body: TestEmailResponse{Status: "queued", To: in.Body.To, Delivery: delivery}}, nil
}

func (h *mailHandler) listSuppressions(ctx context.Context, in *listSuppressionsInput) (*suppressionPageOutput, error) {
	page, err := h.svc.ListSuppressions(ctx, suppressionpg.Filter{Reason: suppressionpg.Reason(in.Reason), Limit: in.Limit, Cursor: in.Cursor})
	if err != nil {
		return nil, err
	}
	out := &suppressionPageOutput{Body: SuppressionPage{Suppressions: make([]SuppressionResponse, len(page.Suppressions)), NextCursor: page.NextCursor}}
	for i, s := range page.Suppressions {
		out.Body.Suppressions[i] = suppressionResponse(s)
	}
	return out, nil
}

func (h *mailHandler) removeSuppression(ctx context.Context, in *removeSuppressionInput) (*suppressionOutput, error) {
	removed, err := h.svc.RemoveSuppression(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, err
	}
	return &suppressionOutput{Body: suppressionResponse(removed)}, nil
}

func suppressionResponse(s suppressionpg.Suppression) SuppressionResponse {
	return SuppressionResponse{
		ID: s.ID, Email: s.Email, Reason: string(s.Reason), Source: s.Source, Detail: s.Detail, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}
