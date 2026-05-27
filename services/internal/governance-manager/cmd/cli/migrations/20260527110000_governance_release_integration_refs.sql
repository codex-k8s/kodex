-- +goose Up
ALTER TABLE governance_manager_release_decision_packages
    ADD COLUMN integration_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT governance_manager_release_packages_integration_refs_chk
        CHECK (jsonb_typeof(integration_refs) = 'array');

-- +goose Down
ALTER TABLE governance_manager_release_decision_packages
    DROP CONSTRAINT IF EXISTS governance_manager_release_packages_integration_refs_chk,
    DROP COLUMN IF EXISTS integration_refs;
