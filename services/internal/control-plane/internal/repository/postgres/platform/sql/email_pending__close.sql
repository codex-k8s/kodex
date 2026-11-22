-- name: email_pending__close :many
WITH candidates AS MATERIALIZED (
  SELECT invocation.id,EXISTS(SELECT 1 FROM control_plane.owner_gates gate
    WHERE gate.integration_invocation_id=invocation.id AND gate.organization_id=invocation.organization_id AND gate.state='OPEN') AS open_gate
  FROM control_plane.integration_invocations invocation
  JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
    AND connection.organization_id=invocation.organization_id AND connection.definition_key='email'
  WHERE invocation.organization_id=@organization_id::uuid
    AND (@connection_ref='' OR connection.ref=@connection_ref)
    AND invocation.effect_receipt_id IS NULL
    AND ((invocation.state IN ('READY','WAITING_APPROVAL') AND invocation.lease_ref IS NULL)
      OR (invocation.state='RUNNING' AND invocation.risk='READ'))
    AND (invocation.generation=0 OR invocation.risk='READ')
    AND (invocation.risk='READ' OR NOT EXISTS(SELECT 1 FROM control_plane.email_authorizations authz
      WHERE authz.organization_id=invocation.organization_id AND authz.invocation_id=invocation.id))
    AND NOT control_plane.email_invocation_mailbox_current(invocation.id)
  ORDER BY invocation.created_at,invocation.id
  FOR UPDATE OF invocation SKIP LOCKED LIMIT 100
), closed AS (
  UPDATE control_plane.integration_invocations invocation
  SET state=CASE WHEN candidates.open_gate THEN 'CANCELLED' ELSE 'FAILED' END,
    safe_error_code='INTEGRATION_AUTHORIZATION_DENIED',
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=invocation.version+1,updated_at=clock_timestamp()
  FROM candidates WHERE invocation.id=candidates.id
  RETURNING invocation.id,invocation.ref,invocation.run_id,invocation.node_id,invocation.state
), gates AS (
  UPDATE control_plane.owner_gates gate
  SET state='CANCELLED',decision='CANCEL',decision_comment='i18n:INTEGRATION_ACTION_FAILED',
    resolved_by=@actor_id::uuid,resolved_at=clock_timestamp(),version=gate.version+1
  WHERE gate.organization_id=@organization_id::uuid AND gate.integration_invocation_id IN (SELECT id FROM closed) AND gate.state='OPEN'
  RETURNING gate.id,gate.ref,gate.node_id,gate.integration_invocation_id
), gate_nodes AS (
  UPDATE control_plane.run_nodes node SET state='CANCELLED',finished_at=clock_timestamp(),version=version+1,next_actions=ARRAY['OPEN']
  WHERE node.organization_id=@organization_id::uuid AND node.id IN (SELECT node_id FROM gates) AND node.state='WAITING'
), deliveries AS (
  UPDATE control_plane.interaction_deliveries delivery
  SET state=CASE WHEN delivery.state='CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
    safe_error_code=CASE WHEN delivery.state='CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE delivery.safe_error_code END,
    lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    completed_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp()
  WHERE delivery.organization_id=@organization_id::uuid AND delivery.gate_id IN (SELECT id FROM gates)
    AND delivery.state IN ('WAITING_APPROVAL','DUE','FAILED','CLAIMED')
), facts AS (
  SELECT 'INVOCATION'::text AS kind,closed.ref,closed.id AS invocation_id,closed.node_id,closed.run_id,closed.state FROM closed
  UNION ALL
  SELECT 'GATE',gates.ref,gates.integration_invocation_id,gates.node_id,closed.run_id,'CANCELLED'
  FROM gates JOIN closed ON closed.id=gates.integration_invocation_id
)
SELECT facts.kind,facts.ref,facts.invocation_id::text,node.ref,COALESCE(run.project_id::text,''),run.root_run_id::text,facts.state,root.state
FROM facts JOIN control_plane.run_nodes node ON node.id=facts.node_id
JOIN control_plane.runs run ON run.id=facts.run_id
JOIN control_plane.runs root ON root.id=run.root_run_id
ORDER BY facts.kind,facts.ref;
