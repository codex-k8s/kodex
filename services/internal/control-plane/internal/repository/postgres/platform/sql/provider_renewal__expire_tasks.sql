-- name: provider_renewal__expire_tasks :exec
WITH expired AS (
    SELECT task.id FROM control_plane.provider_credential_renewal_tasks task
    WHERE task.organization_id = $1::uuid AND task.state IN ('CLAIMED', 'AUTHORIZED') AND task.expires_at <= clock_timestamp()
    ORDER BY task.expires_at, task.id LIMIT 16 FOR UPDATE SKIP LOCKED
)
UPDATE control_plane.provider_credential_renewal_tasks task
SET state = CASE WHEN task.authorized_at IS NULL THEN 'FAILED' ELSE 'UNKNOWN_OUTCOME' END,
    completed_at = clock_timestamp()
FROM expired WHERE task.id = expired.id;
