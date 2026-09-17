package delivery

import (
	"context"
	"time"
)

// ShelfResponse is a shelf as the API returns it.
type ShelfResponse struct {
	ID        string    `json:"id" example:"shf_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name      string    `json:"name" example:"Reading"`
	CreatedAt time.Time `json:"created_at"`
}

type shelfListOutput struct {
	Body struct {
		Items []ShelfResponse `json:"items"`
	}
}

func (h handlers) listShelves(ctx context.Context, _ *struct{}) (*shelfListOutput, error) {
	shelves, err := h.svc.ListShelves(ctx)
	if err != nil {
		return nil, err
	}
	out := &shelfListOutput{}
	out.Body.Items = make([]ShelfResponse, 0, len(shelves))
	for _, s := range shelves {
		out.Body.Items = append(out.Body.Items, ShelfResponse{ID: s.ID, Name: s.Name, CreatedAt: s.CreatedAt})
	}
	return out, nil
}
