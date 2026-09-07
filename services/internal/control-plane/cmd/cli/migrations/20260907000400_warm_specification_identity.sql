-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.assistant_runtime
    ADD COLUMN desired_spec_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN desired_spec_digest text NOT NULL DEFAULT '',
    ADD COLUMN desired_spec_ref text NOT NULL DEFAULT '',
    ADD CONSTRAINT assistant_runtime_desired_spec_valid CHECK (
        (desired_spec_version = 0 AND desired_spec_digest = '' AND desired_spec_ref = '') OR
        (desired_spec_version > 0 AND desired_spec_digest ~ '^[a-f0-9]{64}$' AND desired_spec_ref <> '')
    );
RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
ALTER TABLE control_plane.assistant_runtime
    DROP CONSTRAINT assistant_runtime_desired_spec_valid,
    DROP COLUMN desired_spec_ref,
    DROP COLUMN desired_spec_digest,
    DROP COLUMN desired_spec_version;
RESET ROLE;
