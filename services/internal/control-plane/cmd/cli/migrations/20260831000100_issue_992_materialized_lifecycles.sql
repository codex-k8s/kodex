-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.schedules
    DROP CONSTRAINT schedules_lifecycle_state_check,
    DROP CONSTRAINT schedules_archived_disabled_check,
    ADD CONSTRAINT schedules_lifecycle_state_check
        CHECK (lifecycle_state IN ('ACTIVE', 'ARCHIVED', 'DELETED')),
    ADD CONSTRAINT schedules_terminal_disabled_check
        CHECK (lifecycle_state = 'ACTIVE' OR NOT enabled);

ALTER TABLE control_plane.provider_accounts
    DROP CONSTRAINT provider_accounts_state_check,
    ADD CONSTRAINT provider_accounts_state_check
        CHECK (state IN ('PENDING_AUTHORIZATION', 'AUTHORIZED', 'REAUTHORIZATION_REQUIRED', 'REVOKED', 'DISABLED'));

UPDATE control_plane.provider_accounts
SET state = CASE
        WHEN current_credential_revision_id IS NULL THEN 'REAUTHORIZATION_REQUIRED'
        ELSE 'DISABLED'
    END
WHERE state = 'AUTHORIZED'
  AND NOT enabled;

UPDATE control_plane.provider_accounts
SET enabled = false
WHERE state <> 'AUTHORIZED';

UPDATE control_plane.provider_accounts
SET current_credential_revision_id = NULL
WHERE state = 'REVOKED'
  AND current_credential_revision_id IS NOT NULL;

ALTER TABLE control_plane.provider_accounts
    ADD CONSTRAINT provider_accounts_state_enabled_check
        CHECK (
            (state <> 'AUTHORIZED' OR enabled)
            AND (state NOT IN ('DISABLED', 'REVOKED') OR NOT enabled)
        ),
    ADD CONSTRAINT provider_accounts_credential_lifecycle_check
        CHECK (
            (state <> 'DISABLED' OR current_credential_revision_id IS NOT NULL)
            AND (state <> 'REVOKED' OR current_credential_revision_id IS NULL)
        );

RESET ROLE;
