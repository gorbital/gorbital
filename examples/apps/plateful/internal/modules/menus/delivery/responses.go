package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/menus/domain"
)

// MenuItemResponse is a menu item as the API returns it.
type MenuItemResponse struct {
	ID          string `json:"id" example:"mnu_mfrggzdfmztwq2lkmfrggzdfmy"`
	Section     string `json:"section" example:"Starters"`
	Position    int    `json:"position" doc:"Where the dish sits in its section, smallest first"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// PriceMinor is the price in integer minor units, never a decimal
	// number: 950 is £9.50. A float cannot represent 0.10 exactly, so a bill
	// added up from floats would not come to the right total; clients should
	// divide by the currency's minor units only to display it.
	PriceMinor int64  `json:"price_minor" example:"950" doc:"Price in minor units, such as pence: 950 is £9.50"`
	Currency   string `json:"currency" example:"GBP" doc:"ISO 4217 code the price is in"`
	Available  bool   `json:"available" doc:"False when the kitchen has run out; such dishes are hidden from customers"`
	// Stock is absent when the dish is unlimited.
	Stock        *int      `json:"stock,omitempty" doc:"Portions left; absent when the dish is unlimited"`
	Dietary      []string  `json:"dietary" doc:"Dietary flags, deduplicated and sorted"`
	PhotoImageID string    `json:"photo_image_id,omitempty" doc:"The image the dish is shown with"`
	CreatedBy    string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who added the dish"`
	Version      int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type menuItemOutput struct {
	Body MenuItemResponse
}

func toResponse(item domain.Item) MenuItemResponse {
	return MenuItemResponse{
		ID:           item.ID,
		Section:      item.Section,
		Position:     item.Position,
		Name:         item.Name,
		Description:  item.Description,
		PriceMinor:   item.PriceMinor,
		Currency:     item.Currency,
		Available:    item.Available,
		Stock:        item.Stock,
		Dietary:      item.Dietary,
		PhotoImageID: item.PhotoImageID,
		CreatedBy:    item.CreatedBy,
		Version:      item.Version,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the menu item is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
