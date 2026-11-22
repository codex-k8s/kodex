-- name: email_invocation__completion_source :one
SELECT invocation.risk='READ' AND invocation.state<>'SUCCEEDED'
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
  AND connection.organization_id=invocation.organization_id AND connection.definition_key='email'
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref;
