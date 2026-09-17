// Package delivery is the HTTP adapter of the operations module.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/settings"

	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// SettingResponse is a runtime setting.
type SettingResponse struct {
	Key                string         `json:"key" example:"example.ping_message"`
	Kind               string         `json:"kind" example:"string"`
	Group              string         `json:"group" example:"example"`
	Description        string         `json:"description"`
	Value              any            `json:"value" doc:"Effective value"`
	Default            any            `json:"default" doc:"Value declared in code"`
	Modified           bool           `json:"modified" doc:"A stored value overrides the default"`
	InvalidStoredValue bool           `json:"invalid_stored_value" doc:"The stored value fails validation, so the default applies"`
	Version            int64          `json:"version" doc:"Send back when changing the setting"`
	UpdatedAt          *time.Time     `json:"updated_at,omitempty"`
	UpdatedBy          string         `json:"updated_by,omitempty"`
	ReasonRequired     bool           `json:"reason_required"`
	RestartRequired    bool           `json:"restart_required"`
	RestartPending     bool           `json:"restart_pending"`
	Constraints        map[string]any `json:"constraints,omitempty" doc:"Validation summary such as min, max, one_of, max_len"`
	OrgOverridable     bool           `json:"org_overridable" doc:"Organisations may set their own value; see GET /ops/settings/{key}/overrides"`
}

// SettingList is a list of runtime settings.
type SettingList struct {
	Settings []SettingResponse `json:"settings"`
}

// SettingChange is one change to a runtime setting.
type SettingChange struct {
	ID        int64     `json:"id"`
	Key       string    `json:"key"`
	OldValue  any       `json:"old_value" doc:"null means the default"`
	NewValue  any       `json:"new_value" doc:"null means the default"`
	Version   int64     `json:"version"`
	Reason    string    `json:"reason,omitempty"`
	ActorKind string    `json:"actor_kind"`
	ActorID   string    `json:"actor_id"`
	RequestID string    `json:"request_id,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}

// SettingHistory is a page of setting changes, newest first.
type SettingHistory struct {
	Changes []SettingChange `json:"changes"`
}

// SettingOverride is an organisation's own value of a runtime setting.
type SettingOverride struct {
	OrgID              string     `json:"org_id" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Value              any        `json:"value" doc:"The organisation's value"`
	InvalidStoredValue bool       `json:"invalid_stored_value" doc:"The value fails validation, so the organisation gets the platform value"`
	Version            int64      `json:"version"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
	UpdatedBy          string     `json:"updated_by,omitempty"`
}

// SettingOverrides is a page of organisations' own values of a setting, by
// organisation ID.
type SettingOverrides struct {
	Overrides []SettingOverride `json:"overrides"`
}

type settingOutput struct{ Body SettingResponse }

type settingListOutput struct{ Body SettingList }

type settingHistoryOutput struct{ Body SettingHistory }

type settingOverridesOutput struct{ Body SettingOverrides }

type listSettingsInput struct {
	Group string `query:"group" maxLength:"100" doc:"Only settings in this group"`
}

type settingKeyInput struct {
	Key string `path:"key" maxLength:"200" example:"example.ping_message"`
}

type setSettingInput struct {
	Key  string `path:"key" maxLength:"200" example:"example.ping_message"`
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Value   any      `json:"value" doc:"New value, as JSON of the setting's kind"`
		Version int64    `json:"version" minimum:"0" doc:"Version last read"`
		Reason  string   `json:"reason,omitempty" maxLength:"500" doc:"Why; required when reason_required"`
	}
}

type resetSettingInput struct {
	Key  string `path:"key" maxLength:"200" example:"example.ping_message"`
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Version int64    `json:"version" minimum:"0" doc:"Version last read"`
		Reason  string   `json:"reason,omitempty" maxLength:"500"`
	}
}

type historyInput struct {
	Key    string `path:"key" maxLength:"200" example:"example.ping_message"`
	Before int64  `query:"before" minimum:"0" doc:"Return changes older than this change ID"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type overridesInput struct {
	Key   string `path:"key" maxLength:"200" example:"orgs.invitation_ttl"`
	After string `query:"after" maxLength:"100" doc:"Return organisations after this organisation ID"`
	Limit int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type settingsHandler struct {
	svc *opsusecase.Service
}

// RegisterSettings adds the runtime settings operations to api.
func RegisterSettings(r *gorbital.Router, svc *opsusecase.Service) {
	h := &settingsHandler{svc: svc}
	tags := []string{"Ops: settings"}
	readErrors := []int{http.StatusUnauthorized, http.StatusForbidden}

	operation.Register(r, huma.Operation{
		OperationID: "ops-list-settings", Method: http.MethodGet, Path: "/ops/settings",
		Summary: "List runtime settings", Tags: tags, Security: openapi.Bearer, Errors: readErrors,
	}, h.list)
	operation.Register(r, huma.Operation{
		OperationID: "ops-get-setting", Method: http.MethodGet, Path: "/ops/settings/{key}",
		Summary: "Get a runtime setting", Tags: tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound),
	}, h.get)
	operation.Register(r, huma.Operation{
		OperationID: "ops-set-setting", Method: http.MethodPut, Path: "/ops/settings/{key}",
		Summary:     "Change a runtime setting",
		Description: "Applies to every instance within moments. Send the `version` you read; a newer version returns `setting_version_conflict`.",
		Tags:        tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
	}, h.set)
	operation.Register(r, huma.Operation{
		OperationID: "ops-reset-setting", Method: http.MethodDelete, Path: "/ops/settings/{key}",
		Summary: "Reset a runtime setting to its default", Tags: tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
	}, h.reset)
	operation.Register(r, huma.Operation{
		OperationID: "ops-setting-history", Method: http.MethodGet, Path: "/ops/settings/{key}/history",
		Summary: "List a runtime setting's changes", Tags: tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound),
	}, h.history)
	operation.Register(r, huma.Operation{
		OperationID: "ops-setting-overrides", Method: http.MethodGet, Path: "/ops/settings/{key}/overrides",
		Summary:     "List organisations' own values of a runtime setting",
		Description: "Only settings with `org_overridable` have any (ADR-0056). Ordered by organisation ID; pass the last `org_id` as `after` for the next page.",
		Tags:        tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound),
	}, h.overrides)
}

func (h *settingsHandler) list(ctx context.Context, in *listSettingsInput) (*settingListOutput, error) {
	views, err := h.svc.ListSettings(ctx, in.Group)
	if err != nil {
		return nil, err
	}
	out := &settingListOutput{Body: SettingList{Settings: make([]SettingResponse, len(views))}}
	for i, v := range views {
		out.Body.Settings[i] = settingResponse(v)
	}
	return out, nil
}

func (h *settingsHandler) get(ctx context.Context, in *settingKeyInput) (*settingOutput, error) {
	v, err := h.svc.GetSetting(ctx, in.Key)
	if err != nil {
		return nil, err
	}
	return &settingOutput{Body: settingResponse(v)}, nil
}

func (h *settingsHandler) set(ctx context.Context, in *setSettingInput) (*settingOutput, error) {
	value, err := json.Marshal(in.Body.Value)
	if err != nil {
		return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_setting_value", "value must be JSON")
	}
	v, err := h.svc.SetSetting(ctx, in.Key, value, settings.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, settingError(err)
	}
	return &settingOutput{Body: settingResponse(v)}, nil
}

func (h *settingsHandler) reset(ctx context.Context, in *resetSettingInput) (*settingOutput, error) {
	v, err := h.svc.ResetSetting(ctx, in.Key, settings.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, settingError(err)
	}
	return &settingOutput{Body: settingResponse(v)}, nil
}

func (h *settingsHandler) history(ctx context.Context, in *historyInput) (*settingHistoryOutput, error) {
	entries, err := h.svc.SettingHistory(ctx, in.Key, in.Before, in.Limit)
	if err != nil {
		return nil, err
	}
	out := &settingHistoryOutput{Body: SettingHistory{Changes: make([]SettingChange, len(entries))}}
	for i, e := range entries {
		out.Body.Changes[i] = SettingChange{
			ID: e.ID, Key: e.Key, OldValue: decodeJSON(e.OldValue), NewValue: decodeJSON(e.NewValue),
			Version: e.Version, Reason: e.Reason, ActorKind: string(e.ActorKind), ActorID: e.ActorID,
			RequestID: e.RequestID, ChangedAt: e.ChangedAt,
		}
	}
	return out, nil
}

func (h *settingsHandler) overrides(ctx context.Context, in *overridesInput) (*settingOverridesOutput, error) {
	views, err := h.svc.SettingOverrides(ctx, in.Key, in.After, in.Limit)
	if err != nil {
		return nil, err
	}
	out := &settingOverridesOutput{Body: SettingOverrides{Overrides: make([]SettingOverride, len(views))}}
	for i, v := range views {
		o := SettingOverride{
			OrgID: v.OrgID, Value: decodeJSON(v.Value), InvalidStoredValue: v.InvalidStoredValue,
			Version: v.Version, UpdatedBy: v.UpdatedBy,
		}
		if !v.UpdatedAt.IsZero() {
			o.UpdatedAt = &v.UpdatedAt
		}
		out.Body.Overrides[i] = o
	}
	return out, nil
}

// settingError turns a rejected value into a problem carrying the reason,
// which never includes the value itself.
func settingError(err error) error {
	var invalid *settings.InvalidValueError
	if errors.As(err, &invalid) {
		return httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_setting_value", invalid.Reason)
	}
	return err
}

func settingResponse(v settings.View) SettingResponse {
	r := SettingResponse{
		Key: v.Key, Kind: string(v.Kind), Group: v.Group, Description: v.Description,
		Value: decodeJSON(v.Value), Default: decodeJSON(v.Default),
		Modified: v.Modified, InvalidStoredValue: v.InvalidStoredValue,
		Version: v.Version, UpdatedBy: v.UpdatedBy,
		ReasonRequired: v.ReasonRequired, RestartRequired: v.RestartRequired, RestartPending: v.RestartPending,
		Constraints: v.Constraints, OrgOverridable: v.OrgOverridable,
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
