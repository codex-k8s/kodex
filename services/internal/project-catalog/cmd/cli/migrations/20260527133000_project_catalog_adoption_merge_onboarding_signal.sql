-- +goose Up
ALTER TABLE project_catalog_onboarding_signal_reconciliations
    DROP CONSTRAINT project_catalog_onboarding_signal_kind_chk;

ALTER TABLE project_catalog_onboarding_signal_reconciliations
    ADD CONSTRAINT project_catalog_onboarding_signal_kind_chk
        CHECK (signal_kind IN ('bootstrap_merge', 'adoption_scan', 'adoption_merge'));

-- +goose Down
ALTER TABLE project_catalog_onboarding_signal_reconciliations
    DROP CONSTRAINT project_catalog_onboarding_signal_kind_chk;

ALTER TABLE project_catalog_onboarding_signal_reconciliations
    ADD CONSTRAINT project_catalog_onboarding_signal_kind_chk
        CHECK (signal_kind IN ('bootstrap_merge', 'adoption_scan'));
