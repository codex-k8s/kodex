-- name: model_catalog__expire_tasks :exec
WITH expired AS (
    SELECT task.id FROM control_plane.provider_model_catalog_tasks task
    JOIN control_plane.provider_accounts account ON account.id = task.provider_account_id
    WHERE task.state IN ('PENDING', 'CLAIMED')
      AND ((task.expires_at IS NOT NULL AND task.expires_at <= clock_timestamp())
           OR NOT account.enabled OR account.state <> 'AUTHORIZED'
           OR account.version <> task.account_version
           OR account.current_credential_revision_id IS DISTINCT FROM task.provider_credential_revision_id)
    ORDER BY task.created_at, task.id LIMIT 128 FOR UPDATE OF account, task SKIP LOCKED
)
UPDATE control_plane.provider_model_catalog_tasks task
SET state = 'CANCELLED', claimant_id = '', fence = '', request_digest = '', expires_at = NULL, completed_at = clock_timestamp()
FROM expired WHERE task.id = expired.id;
