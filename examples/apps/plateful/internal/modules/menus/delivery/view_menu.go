package delivery

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

type viewMenuInput struct {
	RestaurantID string `path:"restaurantId" maxLength:"64" example:"rst_mfrggzdfmztwq2lkmfrggzdfmy"`
}

// MenuSection is one heading of a published menu with its dishes.
type MenuSection struct {
	Name  string             `json:"name" example:"Starters"`
	Items []MenuItemResponse `json:"items"`
}

// Menu is a restaurant's published menu: its sections in the order they are
// printed.
type Menu struct {
	Sections []MenuSection `json:"sections"`
}

type menuOutput struct {
	Body Menu
}

func (h handlers) viewMenu(ctx context.Context, in *viewMenuInput) (*menuOutput, error) {
	sections, err := h.svc.ViewMenu(ctx, in.RestaurantID)
	if err != nil {
		return nil, err
	}
	out := &menuOutput{Body: Menu{Sections: make([]MenuSection, len(sections))}}
	for i, section := range sections {
		out.Body.Sections[i] = toSection(section)
	}
	return out, nil
}

func toSection(section domain.Section) MenuSection {
	out := MenuSection{Name: section.Name, Items: make([]MenuItemResponse, len(section.Items))}
	for i, item := range section.Items {
		out.Items[i] = toResponse(item)
	}
	return out
}
