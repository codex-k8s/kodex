-- name: ssh_compose__recovery :exec
UPDATE control_plane.integration_invocations SET ssh_compose_recovery=true
WHERE id=@invocation_id::uuid AND state='RUNNING' AND operation='ssh.compose.deploy';
