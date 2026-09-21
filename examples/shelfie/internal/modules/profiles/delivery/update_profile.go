package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/profiles/domain"
)

type updateProfileInput struct {
	Body struct {
		DisplayName string `json:"display_name" minLength:"1" maxLength:"50"`
		Country     string `json:"country,omitempty" pattern:"^[A-Z]{2}$" doc:"ISO 3166-1 alpha-2 code"`
	}
}

func (h handlers) updateProfile(ctx context.Context, in *updateProfileInput) (*profileOutput, error) {
	p, err := h.svc.UpdateProfile(ctx, domain.Fields{DisplayName: in.Body.DisplayName, Country: in.Body.Country})
	if err != nil {
		return nil, err
	}
	return toResponse(p), nil
}
