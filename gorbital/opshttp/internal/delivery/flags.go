package delivery

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/openapi"

	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// FlagTargets are IDs that always get the same answer.
type FlagTargets struct {
	Allow []string `json:"allow" required:"false" maxItems:"1000" doc:"IDs that get true"`
	Deny  []string `json:"deny" required:"false" maxItems:"1000" doc:"IDs that get false; deny wins over allow in the same rule"`
}

// FlagState is how a feature flag decides, rule by rule: enabled, then
// organisations, then users, then the rollout percentage, then the default.
type FlagState struct {
	_          struct{}    `json:"-" additionalProperties:"true"`
	Enabled    bool        `json:"enabled" doc:"false turns the flag off for everyone, keeping the rules for later"`
	Default    bool        `json:"default" doc:"The answer when no other rule applies"`
	Orgs       FlagTargets `json:"orgs" required:"false" doc:"Organisations, for callers acting in one"`
	Users      FlagTargets `json:"users" required:"false" doc:"Users and other authenticated callers, by ID"`
	Percentage *int        `json:"percentage" required:"false" nullable:"true" minimum:"0" maximum:"100" doc:"Share of subjects (the organisation, else the caller) that get true; null for no rollout. Anonymous callers only follow 0 and 100"`
}

// FlagResponse is a feature flag.
type FlagResponse struct {
	Key                string     `json:"key" example:"example.ping_time"`
	Group              string     `json:"group" example:"example"`
	Description        string     `json:"description"`
	Client             bool       `json:"client" doc:"Signed-in clients read it from GET /v1/flags"`
	State              FlagState  `json:"state" doc:"State in effect"`
	DeclaredState      FlagState  `json:"declared_state" doc:"State declared in code, restored by DELETE"`
	Modified           bool       `json:"modified" doc:"A stored state replaces the declared one"`
	InvalidStoredValue bool       `json:"invalid_stored_value" doc:"The stored state fails validation, so the declared state applies"`
	Version            int64      `json:"version" doc:"Send back when changing the flag"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
	UpdatedBy          string     `json:"updated_by,omitempty"`
}

// FlagList is a list of feature flags.
type FlagList struct {
	Flags []FlagResponse `json:"flags"`
}

// FlagChange is one change to a feature flag.
type FlagChange struct {
	ID        int64      `json:"id"`
	Key       string     `json:"key"`
	OldState  *FlagState `json:"old_state" doc:"null means the declared state"`
	NewState  *FlagState `json:"new_state" doc:"null means the declared state"`
	Version   int64      `json:"version"`
	Reason    string     `json:"reason"`
	ActorKind string     `json:"actor_kind"`
	ActorID   string     `json:"actor_id"`
	RequestID string     `json:"request_id,omitempty"`
	ChangedAt time.Time  `json:"changed_at"`
}

// FlagHistory is a page of feature flag changes, newest first.
type FlagHistory struct {
	Changes []FlagChange `json:"changes"`
}

type flagOutput struct{ Body FlagResponse }

type flagListOutput struct{ Body FlagList }

type flagHistoryOutput struct{ Body FlagHistory }

type listFlagsInput struct {
	Group string `query:"group" maxLength:"100" doc:"Only flags in this group"`
}

type flagKeyInput struct {
	Key string `path:"key" maxLength:"200" example:"example.ping_time"`
}

type setFlagInput struct {
	Key  string `path:"key" maxLength:"200" example:"example.ping_time"`
	Body struct {
		_       struct{}  `json:"-" additionalProperties:"true"`
		State   FlagState `json:"state"`
		Version int64     `json:"version" minimum:"0" doc:"Version last read"`
		Reason  string    `json:"reason" maxLength:"500" doc:"Why; required"`
	}
}

type resetFlagInput struct {
	Key  string `path:"key" maxLength:"200" example:"example.ping_time"`
	Body struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Version int64    `json:"version" minimum:"0" doc:"Version last read"`
		Reason  string   `json:"reason" maxLength:"500" doc:"Why; required"`
	}
}

type flagHistoryInput struct {
	Key    string `path:"key" maxLength:"200" example:"example.ping_time"`
	Before int64  `query:"before" minimum:"0" doc:"Return changes older than this change ID"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type flagsHandler struct {
	svc *opsusecase.Service
}

// RegisterFlags adds the feature flag operations to api (ADR-0057).
func RegisterFlags(r *gorbital.Router, svc *opsusecase.Service) {
	h := &flagsHandler{svc: svc}
	tags := []string{"Ops: flags"}
	readErrors := []int{http.StatusUnauthorized, http.StatusForbidden}

	operation.Register(r, huma.Operation{
		OperationID: "ops-list-flags", Method: http.MethodGet, Path: "/ops/flags",
		Summary: "List feature flags", Tags: tags, Security: openapi.Bearer, Errors: readErrors,
	}, h.list)
	operation.Register(r, huma.Operation{
		OperationID: "ops-get-flag", Method: http.MethodGet, Path: "/ops/flags/{key}",
		Summary: "Get a feature flag", Tags: tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound),
	}, h.get)
	operation.Register(r, huma.Operation{
		OperationID: "ops-set-flag", Method: http.MethodPut, Path: "/ops/flags/{key}",
		Summary:     "Change a feature flag",
		Description: "Replaces the flag's whole state; applies to every instance within moments. Send the `version` you read (a newer one returns `flag_version_conflict`) and a `reason`. Lists hold at most 1000 IDs of 1 to 100 visible ASCII characters each, and an ID can't be in both lists of a rule.",
		Tags:        tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
	}, h.set)
	operation.Register(r, huma.Operation{
		OperationID: "ops-reset-flag", Method: http.MethodDelete, Path: "/ops/flags/{key}",
		Summary: "Reset a feature flag to its declared state", Tags: tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
	}, h.reset)
	operation.Register(r, huma.Operation{
		OperationID: "ops-flag-history", Method: http.MethodGet, Path: "/ops/flags/{key}/history",
		Summary: "List a feature flag's changes", Tags: tags, Security: openapi.Bearer,
		Errors: append(readErrors, http.StatusNotFound),
	}, h.history)
}

func (h *flagsHandler) list(ctx context.Context, in *listFlagsInput) (*flagListOutput, error) {
	views, err := h.svc.ListFlags(ctx, in.Group)
	if err != nil {
		return nil, err
	}
	out := &flagListOutput{Body: FlagList{Flags: make([]FlagResponse, len(views))}}
	for i, v := range views {
		out.Body.Flags[i] = flagResponse(v)
	}
	return out, nil
}

func (h *flagsHandler) get(ctx context.Context, in *flagKeyInput) (*flagOutput, error) {
	v, err := h.svc.GetFlag(ctx, in.Key)
	if err != nil {
		return nil, err
	}
	return &flagOutput{Body: flagResponse(v)}, nil
}

func (h *flagsHandler) set(ctx context.Context, in *setFlagInput) (*flagOutput, error) {
	s := in.Body.State
	state := flags.State{
		Enabled: s.Enabled, Default: s.Default, Percentage: s.Percentage,
		Orgs:  flags.Targets{Allow: s.Orgs.Allow, Deny: s.Orgs.Deny},
		Users: flags.Targets{Allow: s.Users.Allow, Deny: s.Users.Deny},
	}
	v, err := h.svc.SetFlag(ctx, in.Key, state, flags.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, flagError(err)
	}
	return &flagOutput{Body: flagResponse(v)}, nil
}

func (h *flagsHandler) reset(ctx context.Context, in *resetFlagInput) (*flagOutput, error) {
	v, err := h.svc.ResetFlag(ctx, in.Key, flags.Change{Version: in.Body.Version, Reason: in.Body.Reason})
	if err != nil {
		return nil, err
	}
	return &flagOutput{Body: flagResponse(v)}, nil
}

func (h *flagsHandler) history(ctx context.Context, in *flagHistoryInput) (*flagHistoryOutput, error) {
	entries, err := h.svc.FlagHistory(ctx, in.Key, in.Before, in.Limit)
	if err != nil {
		return nil, err
	}
	out := &flagHistoryOutput{Body: FlagHistory{Changes: make([]FlagChange, len(entries))}}
	for i, e := range entries {
		out.Body.Changes[i] = FlagChange{
			ID: e.ID, Key: e.Key, OldState: flagStatePtr(e.OldState), NewState: flagStatePtr(e.NewState),
			Version: e.Version, Reason: e.Reason, ActorKind: string(e.ActorKind), ActorID: e.ActorID,
			RequestID: e.RequestID, ChangedAt: e.ChangedAt,
		}
	}
	return out, nil
}

// flagError turns a rejected state into a problem carrying the reason, which
// names the field and never includes an ID.
func flagError(err error) error {
	var invalid *flags.InvalidStateError
	if errors.As(err, &invalid) {
		return httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_flag_state", invalid.Reason)
	}
	return err
}

func flagResponse(v flags.View) FlagResponse {
	r := FlagResponse{
		Key: v.Key, Group: v.Group, Description: v.Description, Client: v.Client,
		State: flagState(v.State), DeclaredState: flagState(v.Default),
		Modified: v.Modified, InvalidStoredValue: v.InvalidStoredValue,
		Version: v.Version, UpdatedBy: v.UpdatedBy,
	}
	if !v.UpdatedAt.IsZero() {
		r.UpdatedAt = &v.UpdatedAt
	}
	return r
}

func flagState(s flags.State) FlagState {
	return FlagState{
		Enabled: s.Enabled, Default: s.Default, Percentage: s.Percentage,
		Orgs:  FlagTargets{Allow: s.Orgs.Allow, Deny: s.Orgs.Deny},
		Users: FlagTargets{Allow: s.Users.Allow, Deny: s.Users.Deny},
	}
}

func flagStatePtr(s *flags.State) *FlagState {
	if s == nil {
		return nil
	}
	state := flagState(*s)
	return &state
}
