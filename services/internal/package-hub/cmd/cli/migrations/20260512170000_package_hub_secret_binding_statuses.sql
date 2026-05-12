-- +goose Up

ALTER TABLE package_hub_package_installations
    DROP CONSTRAINT package_hub_package_installations_secret_status_chk,
    ADD CONSTRAINT package_hub_package_installations_secret_status_chk
        CHECK (secret_binding_status IN ('not_required', 'missing', 'complete', 'invalid', 'partial', 'check_failed'));

-- +goose Down

ALTER TABLE package_hub_package_installations
    DROP CONSTRAINT package_hub_package_installations_secret_status_chk,
    ADD CONSTRAINT package_hub_package_installations_secret_status_chk
        CHECK (secret_binding_status IN ('not_required', 'missing', 'complete', 'invalid'));
