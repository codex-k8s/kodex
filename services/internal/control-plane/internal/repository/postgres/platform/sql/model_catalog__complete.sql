-- name: model_catalog__complete :exec
WITH inserted AS (
    INSERT INTO control_plane.provider_model_catalog_observations
        (task_id, request_digest, receipt_digest, organization_id, provider_account_id, account_version,
         provider_credential_revision_id, source, failure, models, content_digest, observed_at, expires_at)
    SELECT task.id, task.request_digest, $2, task.organization_id, task.provider_account_id, task.account_version,
           task.provider_credential_revision_id, $3, $4, $5::jsonb, $6, $7::timestamptz, $7::timestamptz + interval '15 minutes'
    FROM control_plane.provider_model_catalog_tasks task
    WHERE task.id = $1::uuid AND task.state = 'CLAIMED'
    RETURNING task_id
)
UPDATE control_plane.provider_model_catalog_tasks task
SET state = 'COMPLETED', claimant_id = '', fence = '', request_digest = '', expires_at = NULL, completed_at = clock_timestamp()
FROM inserted WHERE task.id = inserted.task_id;
