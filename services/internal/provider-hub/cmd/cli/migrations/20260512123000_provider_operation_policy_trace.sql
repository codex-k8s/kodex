-- +goose Up
ALTER TABLE provider_hub_operations
    ADD COLUMN operation_policy_context_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN approval_gate_ref_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN provider_version text NOT NULL DEFAULT '';

ALTER TABLE provider_hub_operations
    ADD CONSTRAINT provider_hub_operations_policy_context_chk
        CHECK (jsonb_typeof(operation_policy_context_json) = 'object'),
    ADD CONSTRAINT provider_hub_operations_approval_gate_chk
        CHECK (jsonb_typeof(approval_gate_ref_json) = 'object');

-- +goose Down
ALTER TABLE provider_hub_operations
    DROP CONSTRAINT IF EXISTS provider_hub_operations_policy_context_chk,
    DROP CONSTRAINT IF EXISTS provider_hub_operations_approval_gate_chk,
    DROP COLUMN IF EXISTS provider_version,
    DROP COLUMN IF EXISTS approval_gate_ref_json,
    DROP COLUMN IF EXISTS operation_policy_context_json;
