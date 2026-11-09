-- name: integration_approval__expire_queued :many
WITH candidates AS (
 SELECT invocation.id,approval.revoked_at
 FROM control_plane.integration_invocations invocation
 JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
 JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
 JOIN control_plane.integration_approval_scopes approval ON approval.id=invocation.approval_scope_id
 WHERE invocation.organization_id=$1::uuid AND invocation.state='READY' AND invocation.approval_policy='HUMAN_SCOPED'
  AND (connection.definition_key='ssh-compose')=$4::boolean
  AND definition.adapter_owner=$2 AND definition.execution_route=$3
  AND invocation.effect_receipt_id IS NULL AND (invocation.generation=0 OR invocation.risk='READ')
  AND (approval.revoked_at IS NOT NULL OR approval.expires_at<=clock_timestamp())
 ORDER BY invocation.created_at,invocation.id
 FOR UPDATE OF invocation SKIP LOCKED LIMIT 100
), expired AS (
 UPDATE control_plane.integration_invocations invocation SET state='FAILED',
  safe_error_code=CASE WHEN candidates.revoked_at IS NULL THEN 'INTEGRATION_APPROVAL_EXPIRED' ELSE 'INTEGRATION_APPROVAL_REVOKED' END,
  version=invocation.version+1,updated_at=clock_timestamp()
 FROM candidates WHERE invocation.id=candidates.id
 RETURNING invocation.id,invocation.ref,invocation.run_id,invocation.node_id,invocation.safe_error_code
)
SELECT expired.id::text,expired.ref,run.root_run_id::text,COALESCE(run.project_id::text,''),node.ref,expired.safe_error_code
FROM expired JOIN control_plane.runs run ON run.id=expired.run_id JOIN control_plane.run_nodes node ON node.id=expired.node_id
