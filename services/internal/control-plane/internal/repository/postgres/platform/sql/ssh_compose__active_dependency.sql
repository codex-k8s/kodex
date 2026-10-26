-- name: ssh_compose__active_dependency :one
SELECT EXISTS (
  SELECT 1 FROM control_plane.integration_invocations invocation
  JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
  WHERE invocation.organization_id=@organization_id::uuid
    AND connection.definition_key='ssh-compose'
    AND invocation.state IN ('READY','WAITING_APPROVAL','RUNNING')
    AND (connection.ref=@connection_ref
      OR invocation.ssh_compose_snapshot->'source'->>'connectionRef'=@connection_ref)
);
