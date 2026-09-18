package delivery

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
	"example.com/plateful/internal/modules/menus/usecase"
)

type updateItemInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"mnu_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version     int64   `json:"version" minimum:"1" doc:"The version you read. If the dish changed since, the update fails with menu_item_version_conflict."`
		Section     *string `json:"section,omitempty" minLength:"1" maxLength:"60"`
		Position    *int    `json:"position,omitempty" minimum:"0"`
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100"`
		Description *string `json:"description,omitempty" maxLength:"1000"`
		// PriceMinor is an integer of minor units: 950 is £9.50, never 9.5.
		PriceMinor   *int64    `json:"price_minor,omitempty" minimum:"0" doc:"Price in minor units, such as pence"`
		Currency     *string   `json:"currency,omitempty" minLength:"3" maxLength:"3"`
		Available    *bool     `json:"available,omitempty" doc:"Set false when the kitchen runs out; the dish leaves the customers' menu and keeps its place on yours"`
		Stock        *int      `json:"stock,omitempty" minimum:"0" doc:"Portions left"`
		Unlimited    *bool     `json:"unlimited,omitempty" doc:"Set true to stop counting portions; it clears stock"`
		Dietary      *[]string `json:"dietary,omitempty" doc:"Replaces the flags; any of vegetarian, vegan, gluten_free, halal, nut_free"`
		PhotoImageID *string   `json:"photo_image_id,omitempty" maxLength:"64"`
	}
}

// updateItem turns the body into domain.Changes. Stock needs two fields on
// the wire because absent and null mean different things to it: leaving
// stock out changes nothing, while unlimited: true is how a request says the
// kitchen has stopped counting portions, which is the column's NULL.
func (h handlers) updateItem(ctx context.Context, in *updateItemInput) (*menuItemOutput, error) {
	changes := domain.Changes{
		Section:      in.Body.Section,
		Position:     in.Body.Position,
		Name:         in.Body.Name,
		Description:  in.Body.Description,
		PriceMinor:   in.Body.PriceMinor,
		Currency:     in.Body.Currency,
		Available:    in.Body.Available,
		Dietary:      in.Body.Dietary,
		PhotoImageID: in.Body.PhotoImageID,
	}
	switch {
	case in.Body.Unlimited != nil && *in.Body.Unlimited:
		changes.SetStock = true // and Stock stays nil: unlimited
	case in.Body.Stock != nil:
		changes.SetStock, changes.Stock = true, in.Body.Stock
	}
	item, err := h.svc.UpdateItem(ctx, in.OrgID, in.ID, usecase.UpdateItemInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &menuItemOutput{Body: toResponse(item)}, nil
}
