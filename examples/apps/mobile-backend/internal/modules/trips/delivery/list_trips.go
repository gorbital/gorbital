package delivery

import "context"

type tripListOutput struct {
	Body struct {
		Items []TripResponse `json:"items"`
	}
}

func (h handlers) listTrips(ctx context.Context, _ *struct{}) (*tripListOutput, error) {
	list, err := h.svc.ListTrips(ctx)
	if err != nil {
		return nil, err
	}
	out := &tripListOutput{}
	out.Body.Items = make([]TripResponse, 0, len(list))
	for _, t := range list {
		out.Body.Items = append(out.Body.Items, toResponse(t))
	}
	return out, nil
}
