package delivery

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start accept-handler

// acceptOrder is the restaurant taking an order on. The handler is four
// lines because the two rules that decide it — the state machine, and the
// payment the payments module holds — are in the use case, where a job or a
// command could reach them too.
func (h handlers) acceptOrder(ctx context.Context, in *orgOrderIDInput) (*orderOutput, error) {
	order, err := h.svc.AcceptOrder(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

// docs:end accept-handler

type rejectOrderInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Reason string `json:"reason,omitempty" maxLength:"500" doc:"What the customer is told, and what the audit log keeps"`
	}
}

func (h handlers) rejectOrder(ctx context.Context, in *rejectOrderInput) (*orderOutput, error) {
	order, err := h.svc.RejectOrder(ctx, in.OrgID, in.ID, in.Body.Reason)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

func (h handlers) startPreparing(ctx context.Context, in *orgOrderIDInput) (*orderOutput, error) {
	return h.advance(ctx, in, domain.StatusPreparing)
}

func (h handlers) markReady(ctx context.Context, in *orgOrderIDInput) (*orderOutput, error) {
	return h.advance(ctx, in, domain.StatusReady)
}

func (h handlers) advance(ctx context.Context, in *orgOrderIDInput, to domain.Status) (*orderOutput, error) {
	order, err := h.svc.AdvanceOrder(ctx, in.OrgID, in.ID, to)
	if err != nil {
		return nil, err
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

func (h handlers) collectOrder(ctx context.Context, in *orderIDInput) (*orderOutput, error) {
	return h.courierAdvance(ctx, in, domain.StatusCollected)
}

func (h handlers) deliverOrder(ctx context.Context, in *orderIDInput) (*orderOutput, error) {
	return h.courierAdvance(ctx, in, domain.StatusDelivered)
}

func (h handlers) courierAdvance(ctx context.Context, in *orderIDInput, to domain.Status) (*orderOutput, error) {
	order, err := h.svc.CourierAdvance(ctx, in.ID, to)
	if err != nil {
		return nil, err
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

type cancelOrderInput struct {
	ID   string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Reason string `json:"reason,omitempty" maxLength:"500"`
	}
}

func (h handlers) cancelOrder(ctx context.Context, in *cancelOrderInput) (*orderOutput, error) {
	order, err := h.svc.CancelOrder(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &orderOutput{Body: toResponse(order)}, nil
}
