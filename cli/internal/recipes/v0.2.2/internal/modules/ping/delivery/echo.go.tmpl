package delivery

import "context"

// EchoInput is the echo request.
type EchoInput struct {
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"` // ignore unknown fields
		Message string   `json:"message" maxLength:"500" doc:"Message to echo back" example:"hello"`
	}
}

func (h handlers) echo(ctx context.Context, in *EchoInput) (*PingOutput, error) {
	m, err := h.svc.Echo(ctx, in.Body.Message)
	if err != nil {
		return nil, err
	}
	return &PingOutput{Body: MessageResponse{Message: m.Text()}}, nil
}
