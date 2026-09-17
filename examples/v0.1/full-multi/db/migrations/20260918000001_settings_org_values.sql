-- Per-organisation settings (ADR-0056): the org_id columns reserved by
-- 00001 hold organisation values of settings declared OrgOverridable.

-- +goose Up
-- GET /ops/settings/{key}/overrides lists one setting's organisation values.
CREATE INDEX settings_values_key_org ON settings_values (key, org_id) WHERE org_id IS NOT NULL;

-- An organisation's history of one setting.
CREATE INDEX settings_history_org_key_id ON settings_history (org_id, key, id DESC) WHERE org_id IS NOT NULL;
