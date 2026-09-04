-- +goose Up
-- Версия 00400 принадлежит unit #1027.
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_definitions
    ADD COLUMN adapter_owner text NOT NULL DEFAULT 'integration-gateway'
        CHECK (adapter_owner IN ('integration-gateway', 'interaction-gateway')),
    ADD COLUMN execution_route text NOT NULL DEFAULT 'MANAGED_MCP'
        CHECK (execution_route IN ('MANAGED_MCP', 'INTERACTION')),
    ADD COLUMN adapter_readiness text NOT NULL DEFAULT 'NOT_READY'
        CHECK (adapter_readiness IN ('READY', 'NOT_READY'));

ALTER TABLE control_plane.integration_definitions
    ALTER COLUMN adapter_owner DROP DEFAULT,
    ALTER COLUMN execution_route DROP DEFAULT,
    ALTER COLUMN adapter_readiness DROP DEFAULT;

ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_state_check,
    ADD CONSTRAINT integration_invocations_state_check CHECK (state IN (
        'WAITING_APPROVAL', 'READY', 'RUNNING', 'SUCCEEDED', 'FAILED',
        'REJECTED', 'CANCELLED', 'UNKNOWN_OUTCOME'
    ));

ALTER TABLE control_plane.integration_effect_receipts
    DROP CONSTRAINT integration_effect_receipts_result_summary_check,
    ADD CONSTRAINT integration_effect_receipts_result_summary_check
        CHECK (octet_length(result_summary) <= 65536);

RESET ROLE;
