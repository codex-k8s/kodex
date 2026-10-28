-- name: ssh_compose__store_snapshot :exec
UPDATE control_plane.integration_invocations
SET ssh_compose_snapshot=@snapshot::jsonb
WHERE id=@invocation_id::uuid AND ssh_compose_snapshot IS NULL;
