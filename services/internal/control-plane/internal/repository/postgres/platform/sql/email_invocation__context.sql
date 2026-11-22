-- name: email_invocation__context :one
SELECT invocation.id::text,connection.id::text,connection.ref,root.id::text,
       root.initiated_by::text,actor.ref,root.source,invocation.resource_scope
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
  AND connection.organization_id=invocation.organization_id AND connection.definition_key='email'
JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id
  AND root.root_run_id=root.id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
  AND connection.lifecycle_state='ACTIVE'
FOR UPDATE OF connection;
