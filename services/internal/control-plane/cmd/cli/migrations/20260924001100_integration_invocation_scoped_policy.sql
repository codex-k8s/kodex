-- +goose Up
SET ROLE control_plane_owner;

-- Старый inline CHECK на колонке остался после замены составного CHECK в #010.
-- Расширяем только утверждённую политику; существующие строки валидируются.
ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_approval_policy_check,
    ADD CONSTRAINT integration_invocations_approval_policy_check
        CHECK (approval_policy IN ('NONE', 'HUMAN_EACH_EFFECT', 'HUMAN_SCOPED')) NOT VALID;

ALTER TABLE control_plane.integration_invocations
    VALIDATE CONSTRAINT integration_invocations_approval_policy_check;

RESET ROLE;
