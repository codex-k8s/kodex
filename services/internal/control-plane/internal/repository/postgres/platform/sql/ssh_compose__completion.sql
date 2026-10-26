-- name: ssh_compose__completion :one
SELECT operation,ssh_compose_snapshot,ssh_compose_recovery,workload_instance
FROM control_plane.integration_invocations WHERE id=@invocation_id::uuid;
