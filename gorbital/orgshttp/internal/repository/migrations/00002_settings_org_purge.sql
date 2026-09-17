-- Per-organisation settings (ADR-0056): organisations' own setting values
-- and their history belong to the organisation, so purging it removes them
-- like every other org-scoped row. Change this migration freely until it is
-- released; afterwards, add a new one.

-- +goose Up
ALTER TABLE settings_values
    ADD CONSTRAINT settings_values_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;

ALTER TABLE settings_history
    ADD CONSTRAINT settings_history_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
