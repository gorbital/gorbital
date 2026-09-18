package delivery

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/openapi"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
	authusecase "example.com/invoicing/internal/modules/auth/usecase"
)

// APIKeyResponse is an API key, without the key itself.
type APIKeyResponse struct {
	ID               string     `json:"id" example:"key_nbswy3dpeb3w64tmmq"`
	Name             string     `json:"name" example:"CI deploys"`
	Prefix           string     `json:"prefix" example:"gbk_nbswy3dpeb3w64tmmqaaaaaaaa" doc:"The start of the key, up to its secret: recognise a key by it"`
	ServiceAccountID string     `json:"service_account_id,omitempty" example:"svc_nbswy3dpeb3w64tmmq"`
	Scopes           []string   `json:"scopes" doc:"Permissions the key is limited to; empty: all of its owner's permissions except those needing two-factor authentication"`
	Status           string     `json:"status" enum:"active,expired,revoked"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty" doc:"Updated at most once a minute"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

// CreatedAPIKeyResponse is a new API key with the key, shown once.
type CreatedAPIKeyResponse struct {
	APIKey APIKeyResponse `json:"api_key"`
	Key    string         `json:"key" example:"gbk_nbswy3dpeb3w64tmmqaaaaaaaa_mfrggzdfmztwq2lknnwg23tpobyxe43uov3ho6dzpiaaaaaaaaaaaaaa" doc:"Send it as Authorization: Bearer <key>. It is shown once: store it now."`
}

// APIKeyList is API keys, newest first, including expired and revoked ones
// for 30 days.
type APIKeyList struct {
	APIKeys []APIKeyResponse `json:"api_keys"`
}

// ServiceAccountResponse is a service account.
type ServiceAccountResponse struct {
	ID          string     `json:"id" example:"svc_nbswy3dpeb3w64tmmq"`
	Name        string     `json:"name" example:"Billing sync"`
	Description string     `json:"description"`
	Roles       []string   `json:"roles"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
}

// ServiceAccountList is service accounts, oldest first.
type ServiceAccountList struct {
	ServiceAccounts []ServiceAccountResponse `json:"service_accounts"`
}

// OrgServiceAccountResponse is an organisation's service account.
type OrgServiceAccountResponse struct {
	ID          string     `json:"id" example:"svc_nbswy3dpeb3w64tmmq"`
	OrgID       string     `json:"org_id" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
	Name        string     `json:"name" example:"Billing sync"`
	Description string     `json:"description"`
	Role        string     `json:"role" example:"member"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
}

// OrgServiceAccountList is an organisation's service accounts, oldest first.
type OrgServiceAccountList struct {
	ServiceAccounts []OrgServiceAccountResponse `json:"service_accounts"`
}

type apiKeyListOutput struct{ Body APIKeyList }

// createdAPIKeyOutput is never stored by caches or proxies: it holds the key.
type createdAPIKeyOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         CreatedAPIKeyResponse
}

type serviceAccountOutput struct{ Body ServiceAccountResponse }

type serviceAccountListOutput struct{ Body ServiceAccountList }

type orgServiceAccountOutput struct{ Body OrgServiceAccountResponse }

type orgServiceAccountListOutput struct{ Body OrgServiceAccountList }

// apiKeyBody describes a new key.
type apiKeyBody struct {
	_         struct{}  `json:"-" additionalProperties:"true"`
	Name      string    `json:"name" maxLength:"100" example:"CI deploys"`
	Scopes    []string  `json:"scopes,omitempty" maxItems:"50" doc:"Limit the key to these permissions. Empty: all of its owner's permissions except those needing two-factor authentication, now and later"`
	ExpiresAt time.Time `json:"expires_at" doc:"Required: at least an hour away and at most auth.api_key_max_ttl (90 days by default)"`
	Password  string    `json:"password,omitempty" maxLength:"512" doc:"Your password; not needed when this session verified a second factor in the last 10 minutes"`
}

func (b apiKeyBody) input() authusecase.APIKeyInput {
	return authusecase.APIKeyInput{Name: b.Name, Scopes: b.Scopes, ExpiresAt: b.ExpiresAt}
}

type createAPIKeyInput struct {
	Body apiKeyBody
}

type apiKeyIDInput struct {
	ID string `path:"id" maxLength:"64" example:"key_nbswy3dpeb3w64tmmq"`
}

type serviceAccountBody struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Name        string   `json:"name" maxLength:"100" example:"Billing sync"`
	Description string   `json:"description,omitempty" maxLength:"500"`
	Roles       []string `json:"roles,omitempty" maxItems:"10" doc:"Platform roles; roles that require two-factor authentication can't be given"`
}

type createServiceAccountInput struct {
	Body serviceAccountBody
}

type serviceAccountIDInput struct {
	ID string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
}

type updateServiceAccountInput struct {
	ID   string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
	Body struct {
		_           struct{}  `json:"-" additionalProperties:"true"`
		Name        *string   `json:"name,omitempty" maxLength:"100"`
		Description *string   `json:"description,omitempty" maxLength:"500"`
		Roles       *[]string `json:"roles,omitempty" maxItems:"10"`
		Disabled    *bool     `json:"disabled,omitempty" doc:"true disables the service account and revokes all its keys at once; false enables it again, without its old keys"`
	}
}

type createServiceAccountKeyInput struct {
	ID   string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
	Body apiKeyBody
}

type serviceAccountKeyIDInput struct {
	ID    string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
	KeyID string `path:"keyId" maxLength:"64" example:"key_nbswy3dpeb3w64tmmq"`
}

// registerAPIKeys adds the signed-in user's API keys and the platform's
// service accounts (ADR-0058).
func registerAPIKeys(r *routes, h *handler, signedIn func(huma.Operation) huma.Operation) {
	route(r, signedIn(huma.Operation{
		OperationID: "auth-list-api-keys", Method: http.MethodGet, Path: "/v1/auth/api-keys",
		Summary: "List your API keys", Errors: []int{http.StatusForbidden},
	}), h.listAPIKeys)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-create-api-key", Method: http.MethodPost, Path: "/v1/auth/api-keys",
		Summary: "Create an API key",
		Description: "Returns the key once. Programs send it as `Authorization: Bearer <key>` and act as you, with your current permissions limited to the key's scopes, " +
			"never those of roles that require two-factor authentication (so never `/ops`). Needs a verified address and your password, " +
			"unless this session verified a second factor in the last 10 minutes; with two-factor authentication on, the session must have verified one. " +
			"An API key can't be used to manage your account, sessions or API keys.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}), h.createAPIKey)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-revoke-api-key", Method: http.MethodDelete, Path: "/v1/auth/api-keys/{id}",
		Summary: "Revoke an API key", Description: "The key stops working at once.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}), h.revokeAPIKey)

	ops := func(op huma.Operation) huma.Operation {
		op.Tags, op.Security = []string{"Ops: service accounts"}, openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized, http.StatusForbidden}, op.Errors...)
		return op
	}
	route(r, ops(huma.Operation{
		OperationID: "ops-list-service-accounts", Method: http.MethodGet, Path: "/ops/service-accounts",
		Summary: "List service accounts", Description: "Platform service accounts: non-human principals with platform roles, which authenticate with API keys.",
	}), h.listServiceAccounts)
	route(r, ops(huma.Operation{
		OperationID: "ops-create-service-account", Method: http.MethodPost, Path: "/ops/service-accounts",
		Summary: "Create a service account",
		Description: "Roles that require two-factor authentication, such as the ops roles, can't be given: API keys never reach `/ops`. " +
			"Create keys with `POST /ops/service-accounts/{id}/keys`.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.createServiceAccount)
	route(r, ops(huma.Operation{
		OperationID: "ops-get-service-account", Method: http.MethodGet, Path: "/ops/service-accounts/{id}",
		Summary: "Get a service account", Errors: []int{http.StatusNotFound},
	}), h.getServiceAccount)
	route(r, ops(huma.Operation{
		OperationID: "ops-update-service-account", Method: http.MethodPatch, Path: "/ops/service-accounts/{id}",
		Summary: "Change a service account", Description: "Disabling it revokes all its keys at once.",
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}), h.updateServiceAccount)
	route(r, ops(huma.Operation{
		OperationID: "ops-delete-service-account", Method: http.MethodDelete, Path: "/ops/service-accounts/{id}",
		Summary: "Delete a service account", Description: "Deletes its keys too.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}), h.deleteServiceAccount)
	route(r, ops(huma.Operation{
		OperationID: "ops-list-service-account-keys", Method: http.MethodGet, Path: "/ops/service-accounts/{id}/keys",
		Summary: "List a service account's API keys", Errors: []int{http.StatusNotFound},
	}), h.listServiceAccountKeys)
	route(r, ops(huma.Operation{
		OperationID: "ops-create-service-account-key", Method: http.MethodPost, Path: "/ops/service-accounts/{id}/keys",
		Summary: "Create an API key for a service account",
		Description: "Returns the key once. Scopes must be permissions the service account's roles grant. " +
			"Send your password unless this session verified a second factor in the last 10 minutes.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}), h.createServiceAccountKey)
	route(r, ops(huma.Operation{
		OperationID: "ops-revoke-service-account-key", Method: http.MethodDelete, Path: "/ops/service-accounts/{id}/keys/{keyId}",
		Summary: "Revoke a service account's API key", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}), h.revokeServiceAccountKey)
}

func (h *handler) listAPIKeys(ctx context.Context, _ *struct{}) (*apiKeyListOutput, error) {
	keys, err := h.svc.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	return apiKeyList(keys, time.Now()), nil
}

func (h *handler) createAPIKey(ctx context.Context, in *createAPIKeyInput) (*createdAPIKeyOutput, error) {
	created, err := h.svc.CreateAPIKey(ctx, in.Body.Password, in.Body.input())
	if err != nil {
		return nil, authError(err)
	}
	return createdAPIKey(created), nil
}

func (h *handler) revokeAPIKey(ctx context.Context, in *apiKeyIDInput) (*struct{}, error) {
	return nil, h.svc.RevokeAPIKey(ctx, in.ID)
}

func (h *handler) listServiceAccounts(ctx context.Context, _ *struct{}) (*serviceAccountListOutput, error) {
	accounts, err := h.svc.ListServiceAccounts(ctx, "")
	if err != nil {
		return nil, err
	}
	out := &serviceAccountListOutput{Body: ServiceAccountList{ServiceAccounts: make([]ServiceAccountResponse, len(accounts))}}
	for i, a := range accounts {
		out.Body.ServiceAccounts[i] = serviceAccountResponse(a)
	}
	return out, nil
}

func (h *handler) createServiceAccount(ctx context.Context, in *createServiceAccountInput) (*serviceAccountOutput, error) {
	a, err := h.svc.CreateServiceAccount(ctx, "", authusecase.ServiceAccountInput{Name: in.Body.Name, Description: in.Body.Description, Roles: in.Body.Roles})
	if err != nil {
		return nil, err
	}
	return &serviceAccountOutput{Body: serviceAccountResponse(a)}, nil
}

func (h *handler) getServiceAccount(ctx context.Context, in *serviceAccountIDInput) (*serviceAccountOutput, error) {
	a, err := h.svc.GetServiceAccount(ctx, "", in.ID)
	if err != nil {
		return nil, err
	}
	return &serviceAccountOutput{Body: serviceAccountResponse(a)}, nil
}

func (h *handler) updateServiceAccount(ctx context.Context, in *updateServiceAccountInput) (*serviceAccountOutput, error) {
	a, err := h.svc.UpdateServiceAccount(ctx, "", in.ID, authusecase.ServiceAccountPatch{
		Name: in.Body.Name, Description: in.Body.Description, Roles: in.Body.Roles, Disabled: in.Body.Disabled,
	})
	if err != nil {
		return nil, err
	}
	return &serviceAccountOutput{Body: serviceAccountResponse(a)}, nil
}

func (h *handler) deleteServiceAccount(ctx context.Context, in *serviceAccountIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteServiceAccount(ctx, "", in.ID)
}

func (h *handler) listServiceAccountKeys(ctx context.Context, in *serviceAccountIDInput) (*apiKeyListOutput, error) {
	keys, err := h.svc.ListServiceAccountKeys(ctx, "", in.ID)
	if err != nil {
		return nil, err
	}
	return apiKeyList(keys, time.Now()), nil
}

func (h *handler) createServiceAccountKey(ctx context.Context, in *createServiceAccountKeyInput) (*createdAPIKeyOutput, error) {
	created, err := h.svc.CreateServiceAccountKey(ctx, "", in.ID, in.Body.Password, in.Body.input())
	if err != nil {
		return nil, authError(err)
	}
	return createdAPIKey(created), nil
}

func (h *handler) revokeServiceAccountKey(ctx context.Context, in *serviceAccountKeyIDInput) (*struct{}, error) {
	return nil, h.svc.RevokeServiceAccountKey(ctx, "", in.ID, in.KeyID)
}

type orgIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
}

type createOrgServiceAccountInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
	Body  struct {
		_           struct{} `json:"-" additionalProperties:"true"`
		Name        string   `json:"name" maxLength:"100" example:"Billing sync"`
		Description string   `json:"description,omitempty" maxLength:"500"`
		Role        string   `json:"role" maxLength:"64" example:"member" doc:"An organisation role you could give a member; not owner, nor a role that requires two-factor authentication"`
	}
}

type orgServiceAccountIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
	ID    string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
}

type updateOrgServiceAccountInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
	ID    string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
	Body  struct {
		_           struct{} `json:"-" additionalProperties:"true"`
		Name        *string  `json:"name,omitempty" maxLength:"100"`
		Description *string  `json:"description,omitempty" maxLength:"500"`
		Role        *string  `json:"role,omitempty" maxLength:"64"`
		Disabled    *bool    `json:"disabled,omitempty" doc:"true disables the service account and revokes all its keys at once; false enables it again, without its old keys"`
	}
}

type createOrgServiceAccountKeyInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
	ID    string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
	Body  apiKeyBody
}

type orgServiceAccountKeyIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lknnwg23tpob"`
	ID    string `path:"id" maxLength:"64" example:"svc_nbswy3dpeb3w64tmmq"`
	KeyID string `path:"keyId" maxLength:"64" example:"key_nbswy3dpeb3w64tmmq"`
}

// RegisterOrgServiceAccounts adds organisations' service accounts under
// /v1/orgs/{orgId}/service-accounts (ADR-0058). The organisations module
// (gorbital.dev/gorbital/orgshttp) mounts it through
// Authenticator.OrgServiceAccountRoutes; the use cases need Config.Orgs. A
// nil svc registers the operations without their dependencies, for
// exporting the OpenAPI document.
func RegisterOrgServiceAccounts(router *gorbital.Router, svc *authusecase.Service) {
	h, r := &handler{svc: svc}, routesOn(router)
	org := func(op huma.Operation) huma.Operation {
		op.Tags, op.Security = []string{"Organisations"}, openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound}, op.Errors...)
		return op
	}
	const base = "/v1/orgs/{orgId}/service-accounts"
	route(r, org(huma.Operation{
		OperationID: "orgs-list-service-accounts", Method: http.MethodGet, Path: base,
		Summary: "List the organisation's service accounts",
		Description: "Service accounts are non-human members with one organisation role, which authenticate with API keys and act only in this organisation. " +
			"Owners and admins manage them.",
	}), h.listOrgServiceAccounts)
	route(r, org(huma.Operation{
		OperationID: "orgs-create-service-account", Method: http.MethodPost, Path: base,
		Summary:       "Create a service account",
		Description:   "The role must be one you could give a member, and not owner or a role that requires two-factor authentication.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.createOrgServiceAccount)
	route(r, org(huma.Operation{
		OperationID: "orgs-get-service-account", Method: http.MethodGet, Path: base + "/{id}",
		Summary: "Get a service account",
	}), h.getOrgServiceAccount)
	route(r, org(huma.Operation{
		OperationID: "orgs-update-service-account", Method: http.MethodPatch, Path: base + "/{id}",
		Summary: "Change a service account", Description: "Disabling it revokes all its keys at once.",
		Errors: []int{http.StatusUnprocessableEntity},
	}), h.updateOrgServiceAccount)
	route(r, org(huma.Operation{
		OperationID: "orgs-delete-service-account", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a service account", Description: "Deletes its keys too.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnprocessableEntity},
	}), h.deleteOrgServiceAccount)
	route(r, org(huma.Operation{
		OperationID: "orgs-list-service-account-keys", Method: http.MethodGet, Path: base + "/{id}/keys",
		Summary: "List a service account's API keys",
	}), h.listOrgServiceAccountKeys)
	route(r, org(huma.Operation{
		OperationID: "orgs-create-service-account-key", Method: http.MethodPost, Path: base + "/{id}/keys",
		Summary: "Create an API key for a service account",
		Description: "Returns the key once. Scopes must be permissions the service account's role grants. " +
			"Send your password unless this session verified a second factor in the last 10 minutes.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}), h.createOrgServiceAccountKey)
	route(r, org(huma.Operation{
		OperationID: "orgs-revoke-service-account-key", Method: http.MethodDelete, Path: base + "/{id}/keys/{keyId}",
		Summary: "Revoke a service account's API key", DefaultStatus: http.StatusNoContent,
	}), h.revokeOrgServiceAccountKey)
}

func (h *handler) listOrgServiceAccounts(ctx context.Context, in *orgIDInput) (*orgServiceAccountListOutput, error) {
	accounts, err := h.svc.ListServiceAccounts(ctx, in.OrgID)
	if err != nil {
		return nil, err
	}
	out := &orgServiceAccountListOutput{Body: OrgServiceAccountList{ServiceAccounts: make([]OrgServiceAccountResponse, len(accounts))}}
	for i, a := range accounts {
		out.Body.ServiceAccounts[i] = orgServiceAccountResponse(a)
	}
	return out, nil
}

func (h *handler) createOrgServiceAccount(ctx context.Context, in *createOrgServiceAccountInput) (*orgServiceAccountOutput, error) {
	a, err := h.svc.CreateServiceAccount(ctx, in.OrgID, authusecase.ServiceAccountInput{
		Name: in.Body.Name, Description: in.Body.Description, Roles: []string{in.Body.Role},
	})
	if err != nil {
		return nil, err
	}
	return &orgServiceAccountOutput{Body: orgServiceAccountResponse(a)}, nil
}

func (h *handler) getOrgServiceAccount(ctx context.Context, in *orgServiceAccountIDInput) (*orgServiceAccountOutput, error) {
	a, err := h.svc.GetServiceAccount(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &orgServiceAccountOutput{Body: orgServiceAccountResponse(a)}, nil
}

func (h *handler) updateOrgServiceAccount(ctx context.Context, in *updateOrgServiceAccountInput) (*orgServiceAccountOutput, error) {
	patch := authusecase.ServiceAccountPatch{Name: in.Body.Name, Description: in.Body.Description, Disabled: in.Body.Disabled}
	if in.Body.Role != nil {
		patch.Roles = &[]string{*in.Body.Role}
	}
	a, err := h.svc.UpdateServiceAccount(ctx, in.OrgID, in.ID, patch)
	if err != nil {
		return nil, err
	}
	return &orgServiceAccountOutput{Body: orgServiceAccountResponse(a)}, nil
}

func (h *handler) deleteOrgServiceAccount(ctx context.Context, in *orgServiceAccountIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteServiceAccount(ctx, in.OrgID, in.ID)
}

func (h *handler) listOrgServiceAccountKeys(ctx context.Context, in *orgServiceAccountIDInput) (*apiKeyListOutput, error) {
	keys, err := h.svc.ListServiceAccountKeys(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return apiKeyList(keys, time.Now()), nil
}

func (h *handler) createOrgServiceAccountKey(ctx context.Context, in *createOrgServiceAccountKeyInput) (*createdAPIKeyOutput, error) {
	created, err := h.svc.CreateServiceAccountKey(ctx, in.OrgID, in.ID, in.Body.Password, in.Body.input())
	if err != nil {
		return nil, authError(err)
	}
	return createdAPIKey(created), nil
}

func (h *handler) revokeOrgServiceAccountKey(ctx context.Context, in *orgServiceAccountKeyIDInput) (*struct{}, error) {
	return nil, h.svc.RevokeServiceAccountKey(ctx, in.OrgID, in.ID, in.KeyID)
}

func apiKeyResponse(k authdomain.APIKey, now time.Time) APIKeyResponse {
	return APIKeyResponse{
		ID: k.ID, Name: k.Name, Prefix: "gbk_" + k.LookupID, ServiceAccountID: k.ServiceAccountID, Scopes: orEmpty(k.Scopes),
		Status: k.StatusAt(now), CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt,
	}
}

func apiKeyList(keys []authdomain.APIKey, now time.Time) *apiKeyListOutput {
	out := &apiKeyListOutput{Body: APIKeyList{APIKeys: make([]APIKeyResponse, len(keys))}}
	for i, k := range keys {
		out.Body.APIKeys[i] = apiKeyResponse(k, now)
	}
	return out
}

func createdAPIKey(c authusecase.CreatedAPIKey) *createdAPIKeyOutput {
	return &createdAPIKeyOutput{CacheControl: "no-store", Body: CreatedAPIKeyResponse{APIKey: apiKeyResponse(c.APIKey, time.Now()), Key: c.Key}}
}

func serviceAccountResponse(a authdomain.ServiceAccount) ServiceAccountResponse {
	return ServiceAccountResponse{
		ID: a.ID, Name: a.Name, Description: a.Description, Roles: orEmpty(a.Roles), Disabled: a.Disabled(),
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, DisabledAt: a.DisabledAt,
	}
}

func orgServiceAccountResponse(a authdomain.ServiceAccount) OrgServiceAccountResponse {
	r := OrgServiceAccountResponse{
		ID: a.ID, OrgID: a.OrgID, Name: a.Name, Description: a.Description, Disabled: a.Disabled(),
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, DisabledAt: a.DisabledAt,
	}
	if len(a.Roles) > 0 {
		r.Role = a.Roles[0]
	}
	return r
}
