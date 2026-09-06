-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.agent_config_overlay_versions
    ADD COLUMN diagnostics jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(diagnostics) = 'array' AND jsonb_array_length(diagnostics) <= 16),
    ADD COLUMN schema_revision text NOT NULL DEFAULT '',
    ADD COLUMN schema_digest text NOT NULL DEFAULT '',
    ADD CONSTRAINT config_overlay_schema_pin CHECK (
        (schema_revision = '' AND schema_digest = '') OR
        (schema_digest ~ '^[a-f0-9]{64}$' AND schema_revision = 'cos_' || schema_digest));

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
ALTER TABLE control_plane.agent_config_overlay_versions
    DROP CONSTRAINT config_overlay_schema_pin,
    DROP COLUMN schema_digest,
    DROP COLUMN schema_revision,
    DROP COLUMN diagnostics;

RESET ROLE;
