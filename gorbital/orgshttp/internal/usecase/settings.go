package usecase

import (
	"context"
	"encoding/json"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/settings"
)

// Permissions on an organisation's own runtime settings (ADR-0056). The org
// catalog in orgshttp's module.go grants them to roles. They are
// public API.
const (
	PermSettingsRead  = "orgs.settings.read"
	PermSettingsWrite = "orgs.settings.write"
)

// Settings returns the runtime settings an organisation may set for itself,
// with its own values where it has them. Only settings declared
// settings.OrgOverridable, by any module, are listed.
func (s *Service) Settings(ctx context.Context, orgID orgslib.ID) ([]settings.View, error) {
	if _, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermSettingsRead); err != nil {
		return nil, storeError("list settings", err)
	}
	if s.settings == nil {
		return []settings.View{}, nil
	}
	return s.settings.ListForOrg(string(orgID))
}

// Setting returns one of them.
func (s *Service) Setting(ctx context.Context, orgID orgslib.ID, key string) (settings.View, error) {
	if _, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermSettingsRead); err != nil {
		return settings.View{}, storeError("get setting", err)
	}
	if s.settings == nil {
		return settings.View{}, settings.ErrUnknownSetting
	}
	return s.settings.GetForOrg(string(orgID), key)
}

// SetSetting sets the organisation's own value of a setting. It applies to
// the organisation's requests on every instance within moments.
func (s *Service) SetSetting(ctx context.Context, orgID orgslib.ID, key string, value json.RawMessage, change settings.Change) (settings.View, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermSettingsWrite)
	if err != nil {
		return settings.View{}, storeError("set setting", err)
	}
	if s.settings == nil {
		return settings.View{}, settings.ErrUnknownSetting
	}
	return s.settings.SetForOrg(ctx, string(orgID), key, value, change)
}

// ResetSetting removes the organisation's own value, so the platform value
// applies again.
func (s *Service) ResetSetting(ctx context.Context, orgID orgslib.ID, key string, change settings.Change) (settings.View, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermSettingsWrite)
	if err != nil {
		return settings.View{}, storeError("reset setting", err)
	}
	if s.settings == nil {
		return settings.View{}, settings.ErrUnknownSetting
	}
	return s.settings.ResetForOrg(ctx, string(orgID), key, change)
}

// SettingHistory returns changes to the organisation's own value of a
// setting, newest first.
func (s *Service) SettingHistory(ctx context.Context, orgID orgslib.ID, key string, before int64, limit int) ([]settings.HistoryEntry, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermSettingsRead)
	if err != nil {
		return nil, storeError("setting history", err)
	}
	if s.settings == nil {
		return nil, settings.ErrUnknownSetting
	}
	return s.settings.HistoryForOrg(ctx, string(orgID), key, before, limit)
}
