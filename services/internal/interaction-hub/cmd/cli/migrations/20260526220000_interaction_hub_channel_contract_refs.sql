-- +goose Up
ALTER TABLE interaction_hub_delivery_routes
    ADD COLUMN package_version_ref text NOT NULL DEFAULT '',
    ADD COLUMN callback_route_ref text NOT NULL DEFAULT '',
    ADD COLUMN runtime_ref text NOT NULL DEFAULT '';

ALTER TABLE interaction_hub_delivery_attempts
    ADD COLUMN channel_capability_ref text NOT NULL DEFAULT '',
    ADD COLUMN package_installation_ref text NOT NULL DEFAULT '',
    ADD COLUMN package_version_ref text NOT NULL DEFAULT '',
    ADD COLUMN delivery_command_ref text NOT NULL DEFAULT '',
    ADD COLUMN callback_ref text NOT NULL DEFAULT '',
    ADD COLUMN callback_route_ref text NOT NULL DEFAULT '',
    ADD COLUMN runtime_ref text NOT NULL DEFAULT '',
    ADD COLUMN runtime_job_ref text NOT NULL DEFAULT '',
    ADD COLUMN routing_policy_ref text NOT NULL DEFAULT '';

ALTER TABLE interaction_hub_channel_callbacks
    ADD COLUMN delivery_id text NOT NULL DEFAULT '',
    ADD COLUMN callback_route_ref text NOT NULL DEFAULT '',
    ADD COLUMN gateway_ref text NOT NULL DEFAULT '',
    ADD COLUMN correlation_id text NOT NULL DEFAULT '',
    ADD COLUMN callback_fingerprint text NOT NULL DEFAULT '';

CREATE INDEX interaction_hub_delivery_routes_capability_idx
    ON interaction_hub_delivery_routes (scope_type, scope_ref, channel_capability_ref, status)
    WHERE channel_capability_ref <> '';

CREATE INDEX interaction_hub_delivery_attempts_command_ref_idx
    ON interaction_hub_delivery_attempts (delivery_command_ref)
    WHERE delivery_command_ref <> '';

CREATE INDEX interaction_hub_delivery_attempts_runtime_job_idx
    ON interaction_hub_delivery_attempts (runtime_job_ref)
    WHERE runtime_job_ref <> '';

CREATE INDEX interaction_hub_channel_callbacks_delivery_id_idx
    ON interaction_hub_channel_callbacks (delivery_id, created_at DESC, id)
    WHERE delivery_id <> '';

-- +goose Down
DROP INDEX IF EXISTS interaction_hub_channel_callbacks_delivery_id_idx;
DROP INDEX IF EXISTS interaction_hub_delivery_attempts_runtime_job_idx;
DROP INDEX IF EXISTS interaction_hub_delivery_attempts_command_ref_idx;
DROP INDEX IF EXISTS interaction_hub_delivery_routes_capability_idx;

ALTER TABLE interaction_hub_channel_callbacks
    DROP COLUMN IF EXISTS callback_fingerprint,
    DROP COLUMN IF EXISTS correlation_id,
    DROP COLUMN IF EXISTS gateway_ref,
    DROP COLUMN IF EXISTS callback_route_ref,
    DROP COLUMN IF EXISTS delivery_id;

ALTER TABLE interaction_hub_delivery_attempts
    DROP COLUMN IF EXISTS routing_policy_ref,
    DROP COLUMN IF EXISTS runtime_job_ref,
    DROP COLUMN IF EXISTS runtime_ref,
    DROP COLUMN IF EXISTS callback_route_ref,
    DROP COLUMN IF EXISTS callback_ref,
    DROP COLUMN IF EXISTS delivery_command_ref,
    DROP COLUMN IF EXISTS package_version_ref,
    DROP COLUMN IF EXISTS package_installation_ref,
    DROP COLUMN IF EXISTS channel_capability_ref;

ALTER TABLE interaction_hub_delivery_routes
    DROP COLUMN IF EXISTS runtime_ref,
    DROP COLUMN IF EXISTS callback_route_ref,
    DROP COLUMN IF EXISTS package_version_ref;
