package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/settings"

	"gorbital.dev/gorbital/operation"
)

// OrgSettingResponse is a runtime setting as an organisation sees it.
type OrgSettingResponse struct {
	Key                string         `json:"key" example:"orgs.invitation_ttl"`
	Kind               string         `json:"kind" example:"duration"`
	Group              string         `json:"group" example:"orgs"`
	Description        string         `json:"description"`
	Value              any            `json:"value" doc:"What the organisation gets: its own value, or else the platform value"`
	PlatformValue      any            `json:"platform_value" doc:"What the organisation gets without its own value"`
	Default            any            `json:"default" doc:"Value declared in code"`
	Overridden         bool           `json:"overridden" doc:"The organisation has its own value"`
	InvalidStoredValue bool           `json:"invalid_stored_value" doc:"The organisation's value fails validation, so the platform value applies"`
	Version            int64          `json:"version" doc:"Version of the organisation's value; send it back when changing it"`
	UpdatedAt          *time.Time     `json:"updated_at,omitempty"`
	UpdatedBy          string         `json:"updated_by,omitempty"`
	ReasonRequired     bool           `json:"reason_required"`
	Constraints        map[string]any `json:"constraints,omitempty" doc:"Validation summary such as min, max, one_of, max_len"`
}

// OrgSettingList is the settings an organisation may set for itself.
type OrgSettingList struct {
	Items []OrgSettingResponse `json:"items"`
}

// OrgSettingChange is one change to an organisation's own value.
type OrgSettingChange struct {
	ID        int64     `json:"id"`
	Key       string    `json:"key"`
	OldValue  any       `json:"old_value" doc:"null means the platform value"`
	NewValue  any       `json:"new_value" doc:"null means the platform value"`
	Version   int64     `json:"version"`
	Reason    string    `json:"reason,omitempty"`
	ActorID   string    `json:"actor_id" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy"`
	ChangedAt time.Time `json:"changed_at"`
}

// OrgSettingHistory is a page of changes, newest first.
type OrgSettingHistory struct {
	Items []OrgSettingChange `json:"items"`
}

type (
	orgSettingOutput        struct{ Body OrgSettingResponse }
	orgSettingListOutput    struct{ Body OrgSettingList }
	orgSettingHistoryOutput struct{ Body OrgSettingHistory }
)

type orgSettingInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	Key   string `path:"key" maxLength:"200" example:"orgs.invitation_ttl"`
}

type setOrgSettingInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	Key   string `path:"key" maxLength:"200" example:"orgs.invitation_ttl"`
	Body  struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Value   any      `json:"value" doc:"New value, as JSON of the setting's kind, such as \"72h\""`
		Version int64    `json:"version" minimum:"0" doc:"Version last read"`
		Reason  string   `json:"reason,omitempty" maxLength:"500" doc:"Why; required when reason_required"`
	}
}

type resetOrgSettingInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	Key   string `path:"key" maxLength:"200" example:"orgs.invitation_ttl"`
	Body  struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Version int64    `json:"version" minimum:"0" doc:"Version last read"`
		Reason  string   `json:"reason,omitempty" maxLength:"500"`
	}
}

type orgSettingHistoryInput struct {
	OrgID  string `path:"orgId" maxLength:"64"`
	Key    string `path:"key" maxLength:"200" example:"orgs.invitation_ttl"`
	Before int64  `query:"before" minimum:"0" doc:"Return changes older than this change ID"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

// registerSettings adds the operations on an organisation's own runtime
// settings (ADR-0056). Members read them; owners and admins change them.
func registerSettings(r *gorbital.Router, h *handler, inOrg func(huma.Operation) huma.Operation) {
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-settings-list", Method: http.MethodGet, Path: "/v1/orgs/{orgId}/settings",
		Summary:     "List the organisation's settings",
		Description: "The runtime settings an organisation may set for itself, with the value it gets. Settings it hasn't set follow the platform value.",
	}), h.settings)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-settings-get", Method: http.MethodGet, Path: "/v1/orgs/{orgId}/settings/{key}",
		Summary: "Get an organisation setting",
	}), h.setting)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-settings-set", Method: http.MethodPut, Path: "/v1/orgs/{orgId}/settings/{key}",
		Summary:     "Set the organisation's own value",
		Description: "Within the setting's constraints. Send the `version` you read; a newer version returns `setting_version_conflict`.",
		Errors:      []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.setSetting)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-settings-reset", Method: http.MethodDelete, Path: "/v1/orgs/{orgId}/settings/{key}",
		Summary:     "Go back to the platform value",
		Description: "Removes the organisation's own value (`reason` in the body when required).",
		Errors:      []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.resetSetting)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-settings-history", Method: http.MethodGet, Path: "/v1/orgs/{orgId}/settings/{key}/history",
		Summary: "List changes to an organisation setting",
	}), h.settingHistory)
}

func (h *handler) settings(ctx context.Context, in *orgInput) (*orgSettingListOutput, error) {
	views, err := h.svc.Settings(ctx, orgslib.ID(in.OrgID))
	if err != nil {
		return nil, settingError(err)
	}
	out := &orgSettingListOutput{Body: OrgSettingList{Items: make([]OrgSettingResponse, len(views))}}
	for i, v := range views {
		out.Body.Items[i] = orgSettingResponse(v)
	}
	return out, nil
}

func (h *handler) setting(ctx context.Context, in *orgSettingInput) (*orgSettingOutput, error) {
	v, err := h.svc.Setting(ctx, orgslib.ID(in.OrgID), in.Key)
	if err != nil {
		return nil, settingError(err)
	}
	return &orgSettingOutput{Body: orgSettingResponse(v)}, nil
}

func (h *handler) setSetting(ctx context.Context, in *setOrgSettingInput) (*orgSettingOutput, error) {
	value, err := json.Marshal(in.Body.Value)
	if err != nil {
		return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_setting_value", "value must be JSON")
	}
	v, err := h.svc.SetSetting(ctx, orgslib.ID(in.OrgID), in.Key, value, settings.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, settingError(err)
	}
	return &orgSettingOutput{Body: orgSettingResponse(v)}, nil
}

func (h *handler) resetSetting(ctx context.Context, in *resetOrgSettingInput) (*orgSettingOutput, error) {
	v, err := h.svc.ResetSetting(ctx, orgslib.ID(in.OrgID), in.Key, settings.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, settingError(err)
	}
	return &orgSettingOutput{Body: orgSettingResponse(v)}, nil
}

func (h *handler) settingHistory(ctx context.Context, in *orgSettingHistoryInput) (*orgSettingHistoryOutput, error) {
	entries, err := h.svc.SettingHistory(ctx, orgslib.ID(in.OrgID), in.Key, in.Before, in.Limit)
	if err != nil {
		return nil, settingError(err)
	}
	out := &orgSettingHistoryOutput{Body: OrgSettingHistory{Items: make([]OrgSettingChange, len(entries))}}
	for i, e := range entries {
		out.Body.Items[i] = OrgSettingChange{
			ID: e.ID, Key: e.Key, OldValue: decodeJSON(e.OldValue), NewValue: decodeJSON(e.NewValue),
			Version: e.Version, Reason: e.Reason, ActorID: e.ActorID, ChangedAt: e.ChangedAt,
		}
	}
	return out, nil
}

// settingError turns a rejected value into a problem carrying the reason,
// which never includes the value itself. The settings store's other errors
// get the problems the operations API maps them to (opshttp), so an app
// without /ops answers as a v0.1 app did.
func settingError(err error) error {
	var invalid *settings.InvalidValueError
	switch {
	case errors.As(err, &invalid):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_setting_value", invalid.Reason)
	case errors.Is(err, settings.ErrNotOrgOverridable):
		return err // mapped by the module
	case errors.Is(err, settings.ErrUnknownSetting):
		return httpx.NewProblem(http.StatusNotFound, "setting_not_found", "no setting has this key")
	case errors.Is(err, settings.ErrVersionConflict):
		return httpx.NewProblem(http.StatusConflict, "setting_version_conflict", "the setting changed since it was read; read it again")
	case errors.Is(err, settings.ErrReasonRequired):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "setting_reason_required", "a reason is required to change this setting")
	case errors.Is(err, settings.ErrInvalidValue):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_setting_value", "the value is not valid for this setting")
	}
	return err
}

func orgSettingResponse(v settings.View) OrgSettingResponse {
	r := OrgSettingResponse{
		Key: v.Key, Kind: string(v.Kind), Group: v.Group, Description: v.Description,
		Value: decodeJSON(v.Value), PlatformValue: decodeJSON(v.PlatformValue), Default: decodeJSON(v.Default),
		Overridden: v.Modified, InvalidStoredValue: v.InvalidStoredValue,
		Version: v.Version, UpdatedBy: v.UpdatedBy,
		ReasonRequired: v.ReasonRequired, Constraints: v.Constraints,
	}
	if !v.UpdatedAt.IsZero() {
		r.UpdatedAt = &v.UpdatedAt
	}
	return r
}

func decodeJSON(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
