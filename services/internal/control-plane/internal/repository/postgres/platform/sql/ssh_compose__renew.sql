-- name: ssh_compose__renew :exec
UPDATE control_plane.integration_invocations
SET lease_expires_at=@expires_at,ssh_compose_deadline=COALESCE(ssh_compose_deadline,@deadline),
    version=version+1,updated_at=clock_timestamp()
WHERE id=@invocation_id::uuid AND state='RUNNING';
