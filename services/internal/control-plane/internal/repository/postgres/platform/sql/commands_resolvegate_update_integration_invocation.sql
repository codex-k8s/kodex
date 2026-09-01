-- name: commands_resolvegate_update_integration_invocation :exec
UPDATE control_plane.integration_invocations
SET state=$2,safe_error_code=$3,version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid AND state='WAITING_APPROVAL'
