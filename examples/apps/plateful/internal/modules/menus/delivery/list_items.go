package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/menus/usecase"
)

type listItemsInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Section string `query:"section" maxLength:"60" doc:"Only dishes printed under this heading"`
	// Available has three states, not two: absent is no filter at all, and
	// available=false is a filter of its own, because that is how a kitchen
	// finds what it switched off last night and forgot. Huma does not accept
	// a pointer for a query parameter, so the three states are spelled out
	// as an enum of two values and the empty string, and turned into the
	// *bool the use case wants below.
	Available string `query:"available" enum:"true,false" doc:"Only the dishes that are, or aren't, available"`
}

// MenuItemPage is a page of menu items.
type MenuItemPage struct {
	Items      []MenuItemResponse `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type menuItemPageOutput struct {
	Body MenuItemPage
}

func (h handlers) listItems(ctx context.Context, in *listItemsInput) (*menuItemPageOutput, error) {
	var available *bool
	if in.Available != "" {
		value := in.Available == "true"
		available = &value
	}
	res, err := h.svc.ListItems(ctx, in.OrgID, usecase.ListItemsInput{
		Page:      in.Params,
		Section:   in.Section,
		Available: available,
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &menuItemPageOutput{Body: MenuItemPage{Items: make([]MenuItemResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, item := range res.Items {
		out.Body.Items[i] = toResponse(item)
	}
	return out, nil
}
