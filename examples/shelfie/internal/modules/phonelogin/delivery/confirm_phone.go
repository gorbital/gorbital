package delivery

import "context"

type confirmPhoneInput struct {
	Body struct {
		Phone string `json:"phone" maxLength:"32" example:"+447700900123"`
		Code  string `json:"code" pattern:"^[0-9]{6}$" example:"123456"`
	}
}

func (h handlers) confirmPhone(ctx context.Context, in *confirmPhoneInput) (*struct{}, error) {
	return nil, h.svc.ConfirmPhone(ctx, in.Body.Phone, in.Body.Code)
}
