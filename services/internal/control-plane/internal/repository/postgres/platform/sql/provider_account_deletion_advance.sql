-- name: provider_account_deletion_advance :exec
WITH observed AS MATERIALIZED (
    SELECT intent.id,
        EXISTS (SELECT 1 FROM control_plane.provider_account_blockers(intent.organization_id, intent.provider_account_id)) AS blocked,
        EXISTS (SELECT 1 FROM control_plane.provider_credential_cleanup_tasks task
                WHERE task.organization_id = intent.organization_id AND task.provider_account_id = intent.provider_account_id
                  AND task.state <> 'COMPLETED') AS pending,
        EXISTS (SELECT 1 FROM control_plane.provider_credential_cleanup_tasks task
                WHERE task.organization_id = intent.organization_id AND task.provider_account_id = intent.provider_account_id
                  AND task.state = 'DEAD_LETTER') AS failed,
        EXISTS (SELECT 1 FROM control_plane.provider_credential_cleanup_tasks task
                WHERE task.organization_id = intent.organization_id AND task.provider_account_id = intent.provider_account_id
                  AND task.state = 'CLAIMED') AS cleaning
    FROM control_plane.provider_account_deletion_intents intent
    WHERE intent.organization_id = @organization_id::uuid AND intent.provider_account_id = @account_id::uuid
      AND intent.state <> 'DELETED'
), desired AS (
    SELECT *, CASE WHEN blocked THEN 'PENDING_BLOCKERS' WHEN failed THEN 'FAILED'
                   WHEN cleaning THEN 'CLEANING' WHEN pending THEN 'CLEANUP_QUEUED' ELSE 'DELETED' END AS next_state,
        CASE WHEN blocked THEN 'WAITING_FOR_DEPENDENCIES' WHEN failed THEN 'CREDENTIAL_CLEANUP_FAILED'
             WHEN cleaning THEN 'CREDENTIAL_CLEANUP_IN_PROGRESS' WHEN pending THEN 'CREDENTIAL_CLEANUP_PENDING'
             ELSE 'ACCOUNT_DELETED' END AS next_reason
    FROM observed
), account_change AS (
    UPDATE control_plane.provider_accounts account
    SET current_credential_revision_id = NULL,
        state = CASE WHEN desired.next_state = 'DELETED' THEN 'DELETED' ELSE account.state END,
        version = account.version + 1, updated_at = clock_timestamp()
    FROM desired
    WHERE account.id = @account_id::uuid AND account.organization_id = @organization_id::uuid
      AND NOT desired.blocked
      AND (account.current_credential_revision_id IS NOT NULL OR desired.next_state = 'DELETED')
)
UPDATE control_plane.provider_account_deletion_intents intent
SET state = desired.next_state, safe_reason = desired.next_reason,
    version = intent.version + CASE WHEN intent.state <> desired.next_state THEN 1 ELSE 0 END,
    completed_at = CASE WHEN desired.next_state = 'DELETED' THEN clock_timestamp() ELSE NULL END,
    updated_at = clock_timestamp()
FROM desired WHERE intent.id = desired.id;
