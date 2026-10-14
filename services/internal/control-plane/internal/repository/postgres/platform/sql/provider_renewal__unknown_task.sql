-- name: provider_renewal__unknown_task :exec
UPDATE control_plane.provider_credential_renewal_tasks
SET state = CASE WHEN authorized_at IS NULL THEN 'FAILED' ELSE 'UNKNOWN_OUTCOME' END, completed_at = clock_timestamp()
WHERE id = $1::uuid AND state IN ('CLAIMED', 'AUTHORIZED');
