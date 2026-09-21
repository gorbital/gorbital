package delivery

import "context"

type sendSignInCodeInput struct {
	Body struct {
		Phone string `json:"phone" maxLength:"32" example:"+447700900123"`
	}
}

type sentOutput struct {
	Body struct {
		Status string `json:"status" enum:"check_your_phone"`
	}
}

func (h handlers) sendSignInCode(ctx context.Context, in *sendSignInCodeInput) (*sentOutput, error) {
	if err := h.svc.SendSignInCode(ctx, in.Body.Phone); err != nil {
		return nil, err
	}
	out := &sentOutput{}
	out.Body.Status = "check_your_phone"
	return out, nil
}
