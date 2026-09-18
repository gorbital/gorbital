package delivery

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

// docs:start create-item-handler

type createItemInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Section     string `json:"section" minLength:"1" maxLength:"60" example:"Starters" doc:"The heading the dish is printed under; a new one starts existing here"`
		Position    int    `json:"position,omitempty" minimum:"0" doc:"Where the dish sits in its section, smallest first"`
		Name        string `json:"name" minLength:"1" maxLength:"100" doc:"Unique on your menu, ignoring case"`
		Description string `json:"description,omitempty" maxLength:"1000"`
		// PriceMinor is an integer of minor units, not a decimal number:
		// 950 is £9.50. JSON numbers are doubles in most clients, and a
		// double cannot hold 0.10 exactly, so a price sent as 9.50 would
		// arrive slightly wrong and every total built from it would drift.
		PriceMinor   int64    `json:"price_minor" minimum:"0" example:"950" doc:"Price in minor units, such as pence: 950 is £9.50"`
		Currency     string   `json:"currency,omitempty" minLength:"3" maxLength:"3" example:"GBP" default:"GBP" doc:"ISO 4217 code"`
		Available    *bool    `json:"available,omitempty" doc:"Defaults to true: a dish is added because you mean to sell it"`
		Stock        *int     `json:"stock,omitempty" minimum:"0" doc:"Portions you have; leave it out for a dish you never run out of"`
		Dietary      []string `json:"dietary,omitempty" doc:"Any of vegetarian, vegan, gluten_free, halal, nut_free; stored deduplicated and sorted"`
		PhotoImageID string   `json:"photo_image_id,omitempty" maxLength:"64" doc:"An image you uploaded for this dish"`
	}
}

// createItem is the whole handler: read the request, call the use case,
// shape the answer. Every rule about what a dish may be — the dietary flags
// that exist, the price being a count of minor units, the name being unique
// on the menu — lives in the domain and the use case, where a job or a
// command could reach it too.
func (h handlers) createItem(ctx context.Context, in *createItemInput) (*menuItemOutput, error) {
	available := true
	if in.Body.Available != nil {
		available = *in.Body.Available
	}
	item, err := h.svc.CreateItem(ctx, in.OrgID, domain.ItemFields{
		Section:      in.Body.Section,
		Position:     in.Body.Position,
		Name:         in.Body.Name,
		Description:  in.Body.Description,
		PriceMinor:   in.Body.PriceMinor,
		Currency:     in.Body.Currency,
		Available:    available,
		Stock:        in.Body.Stock,
		Dietary:      in.Body.Dietary,
		PhotoImageID: in.Body.PhotoImageID,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &menuItemOutput{Body: toResponse(item)}, nil
}

// docs:end create-item-handler
