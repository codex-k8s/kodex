-- name: provider_renewal__authorize_task :exec
UPDATE control_plane.provider_credential_renewal_tasks
SET state = 'AUTHORIZED', authorized_at = clock_timestamp()
WHERE id = $1::uuid AND state = 'CLAIMED' AND expires_at > clock_timestamp();
