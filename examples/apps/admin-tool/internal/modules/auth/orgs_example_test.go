package authhttp_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	authhttp "example.com/admin-tool/internal/modules/auth"
	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/openapi"
)

// exampleAPI returns an API with the bearer scheme signed-in routes need.
func exampleAPI() (huma.API, *http.ServeMux) {
	mux := http.NewServeMux()
	return openapi.New(mux, "acme-api", "1.0.0", openapi.WithBearerAuth("Session token")), mux
}

// orgDirectory stands in for an app's organisations. Apps use
// gorbital.dev/gorbital/orgshttp, which implements authhttp.Organisations.
type orgDirectory struct{}

func (orgDirectory) AccountCreated(context.Context, string) error       { return nil }
func (orgDirectory) CheckAccountDeletion(context.Context, string) error { return nil }
func (orgDirectory) AccountDeleted(context.Context, string) error       { return nil }
func (orgDirectory) AuthorizeServiceAccounts(ctx context.Context, _ string) (context.Context, string, error) {
	return ctx, "", authlib.ErrUnauthenticated
}
func (orgDirectory) CanAssignServiceAccountRole(string, string) bool { return false }
func (orgDirectory) Catalog() *authlib.Catalog                       { return authlib.NewCatalog() }

func ExampleOrganisations() {
	// orgshttp connects its organisations to sign-in from its module's
	// Platform function; an app only passes the authenticator on.
	auth := authhttp.New()
	var orgs authhttp.Organisations = orgDirectory{}
	fmt.Println(auth.UseOrganisations(orgs))
	// Output: <nil>
}

func ExampleAuthenticator_UseOrganisations() {
	auth := authhttp.New()
	if err := auth.UseOrganisations(nil); err != nil {
		fmt.Println(err)
	}
	fmt.Println(auth.UseOrganisations(orgDirectory{}))
	// Output:
	// authhttp: UseOrganisations: the organisations are nil
	// <nil>
}

func ExampleAuthenticator_OrgServiceAccountRoutes() {
	// The organisations module registers organisations' service accounts
	// with its own routes.
	auth := authhttp.New()
	orgs := gorbital.Module{
		Name:   "orgs",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) { auth.OrgServiceAccountRoutes(r) },
	}
	api, mux := exampleAPI()
	if err := gorbital.Mount(api, nil, gorbital.Deps{}, orgs); err != nil {
		panic(err)
	}
	for _, path := range []string{"/v1/orgs/{orgId}/service-accounts", "/v1/orgs/{orgId}/service-accounts/{id}/keys"} {
		item := api.OpenAPI().Paths[path]
		fmt.Println(item.Get.OperationID, item.Post.OperationID)
	}
	_ = http.Handler(mux)
	// Output:
	// orgs-list-service-accounts orgs-create-service-account
	// orgs-list-service-account-keys orgs-create-service-account-key
}
