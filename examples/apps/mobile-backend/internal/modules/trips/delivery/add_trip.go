package delivery

import (
	"context"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

type addTripInput struct {
	Body struct {
		Destination string `json:"destination" minLength:"1" maxLength:"120"`
		Notes       string `json:"notes,omitempty" maxLength:"2000"`
	}
}

// docs:start add-handler

func (h handlers) addTrip(ctx context.Context, in *addTripInput) (*tripOutput, error) {
	// No owner in the request: the use case takes it from the actor.
	t, err := h.svc.AddTrip(ctx, domain.Fields{Destination: in.Body.Destination, Notes: in.Body.Notes})
	if err != nil {
		return nil, err
	}
	return &tripOutput{Body: toResponse(t)}, nil
}

// docs:end add-handler
