package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/orders/domain"
)

// OrderResponse is an order as the API returns it.
type OrderResponse struct {
	ID        string    `json:"id" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Status    string    `json:"status" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled"`
	Address   string    `json:"address"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created it"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type orderOutput struct {
	Body OrderResponse
}

// orderIDInput is the path of the routes on one order.
type orderIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(order domain.Order) OrderResponse {
	return OrderResponse{
		ID:        order.ID,
		Status:    string(order.Status),
		Address:   order.Address,
		Note:      order.Note,
		CreatedBy: order.CreatedBy,
		Version:   order.Version,
		CreatedAt: order.CreatedAt,
		UpdatedAt: order.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the order is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
