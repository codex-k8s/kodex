-- name: organization_secret_operation_expire_fixture :exec
UPDATE control_plane.runtime_secret_draft_operations
SET lease_deadline = clock_timestamp() - interval '1 second'
WHERE ref = $1;
