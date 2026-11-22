-- name: kubernetes_invocation__deny_unstarted :one
UPDATE control_plane.integration_invocations invocation
SET state='FAILED',safe_error_code='INTEGRATION_CREDENTIAL_UNAVAILABLE',
 lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 version=invocation.version+1,updated_at=clock_timestamp()
FROM control_plane.integration_connections connection,control_plane.runs execution,control_plane.run_nodes node
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.connection_id=connection.id AND connection.organization_id=invocation.organization_id
 AND connection.definition_key='kubernetes-managed' AND invocation.state='READY' AND invocation.generation=0
 AND execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
 AND node.id=invocation.node_id AND node.organization_id=invocation.organization_id
 AND NOT EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=invocation.id AND gate.state='OPEN')
RETURNING invocation.id::text,COALESCE(execution.project_id::text,''),execution.root_run_id::text,node.ref;
