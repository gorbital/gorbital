// Package profiles is Shelfie's profiles module: a reader's display name and
// country, collected at registration through authhttp.RegisterFields
// (hooks.go), in four layers with one file per operation.
package profiles

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/shelfie/internal/modules/profiles/delivery"
	"example.com/shelfie/internal/modules/profiles/domain"
	"example.com/shelfie/internal/modules/profiles/repository"
	"example.com/shelfie/internal/modules/profiles/usecase"
)

// Module returns the profiles module.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "profiles",
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrProfileIncomplete, Status: http.StatusNotFound, Code: "profile_incomplete", Detail: "create your profile with PUT /v1/profile"},
			{Err: domain.ErrInvalidDisplayName, Status: http.StatusUnprocessableEntity, Code: "invalid_display_name", Detail: "a display name is 1 to 50 characters"},
			{Err: domain.ErrInvalidCountry, Status: http.StatusUnprocessableEntity, Code: "invalid_country", Detail: "a country is an ISO 3166-1 alpha-2 code, such as GB"},
		},
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See your profile", Roles: []string{"user"}},
			{Name: usecase.PermWrite, Description: "Create and change your profile", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			delivery.Register(r, usecase.NewService(repository.NewStore(d.DB)))
		},
	}
}
