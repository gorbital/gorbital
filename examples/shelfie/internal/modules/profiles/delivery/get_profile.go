package delivery

import (
	"context"
	"time"

	"example.com/shelfie/internal/modules/profiles/domain"
)

// ProfileResponse is a profile as the API returns it.
type ProfileResponse struct {
	DisplayName string    `json:"display_name" example:"Ada"`
	Country     string    `json:"country,omitempty" example:"GB"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type profileOutput struct {
	Body ProfileResponse
}

func toResponse(p domain.Profile) *profileOutput {
	return &profileOutput{Body: ProfileResponse{DisplayName: p.DisplayName, Country: p.Country, UpdatedAt: p.UpdatedAt}}
}

func (h handlers) getProfile(ctx context.Context, _ *struct{}) (*profileOutput, error) {
	p, err := h.svc.GetProfile(ctx)
	if err != nil {
		return nil, err
	}
	return toResponse(p), nil
}
