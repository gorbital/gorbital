package delivery

import "context"

type setPhoneInput struct {
	Body struct {
		Phone string `json:"phone" maxLength:"32" example:"+447700900123" doc:"E.164; spaces and dashes are ignored"`
	}
}

func (h handlers) setPhone(ctx context.Context, in *setPhoneInput) (*struct{}, error) {
	return nil, h.svc.SetPhone(ctx, in.Body.Phone)
}
