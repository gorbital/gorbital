package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/notifications/domain"
)

// EndpointResponse is a notification endpoint as the API returns it.
//
// There is no url field, and there will not be one. A Slack-style incoming
// webhook URL is a bearer credential: the path is the whole authentication,
// so anyone who reads it can post into the restaurant's channel for as long
// as it lives. Returning it would mean it appears in browser dev tools, in
// the response caches of every proxy on the way, in the API logs of whatever
// integration a restaurant wires up, and in a support screenshot. Host is
// enough to recognise which channel a row is, and worth nothing to a thief.
type EndpointResponse struct {
	ID    string `json:"id" example:"nte_mfrggzdfmztwq2lkmfrggzdfmy"`
	Label string `json:"label" example:"Kitchen"`
	// Host is where the alerts go, without the path that makes the URL a
	// credential.
	Host string `json:"host" example:"hooks.example.com" doc:"The host alerts are posted to; the URL itself is never returned"`
	// LastDeliveryAt is absent until something has been delivered.
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
	// LastStatus is the HTTP status the endpoint answered last, 0 when it
	// never answered.
	LastStatus int `json:"last_status" example:"200"`
	// LastError is why the last delivery failed, empty when it worked. It
	// names the host and the status, never the URL.
	LastError string    `json:"last_error,omitempty"`
	CreatedBy string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who registered it"`
	CreatedAt time.Time `json:"created_at"`
}

type endpointOutput struct {
	Body EndpointResponse
}

func toResponse(e domain.Endpoint) EndpointResponse {
	res := EndpointResponse{
		ID:         e.ID,
		Label:      e.Label,
		Host:       e.URL.Host(),
		LastStatus: e.LastStatus,
		LastError:  e.LastError,
		CreatedBy:  e.CreatedBy,
		CreatedAt:  e.CreatedAt,
	}
	if !e.LastDeliveryAt.IsZero() {
		at := e.LastDeliveryAt
		res.LastDeliveryAt = &at
	}
	return res
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors, ErrInvalidURL among them, so a URL this
// deployment won't deliver to keeps its own problem code rather than being
// folded into validation_failed.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the notification endpoint is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
