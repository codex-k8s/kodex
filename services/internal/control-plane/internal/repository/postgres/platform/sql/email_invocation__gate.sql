-- name: email_invocation__gate :one
SELECT invocation.ref
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
  AND connection.organization_id=invocation.organization_id AND connection.definition_key='email'
WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
FOR UPDATE OF invocation;
