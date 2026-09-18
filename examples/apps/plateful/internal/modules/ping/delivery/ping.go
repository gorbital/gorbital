package delivery

import "context"

func (h handlers) ping(ctx context.Context, _ *struct{}) (*PingOutput, error) {
	out := &PingOutput{Body: MessageResponse{Message: h.svc.Ping(ctx)}}
	if now, ok := h.svc.ServerTime(ctx); ok {
		out.Body.ServerTime = &now
	}
	return out, nil
}
