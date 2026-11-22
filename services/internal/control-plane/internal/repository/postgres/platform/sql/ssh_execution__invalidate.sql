-- name: ssh_execution__invalidate :many
WITH closed AS (
  UPDATE control_plane.integration_invocations invocation
  SET state=CASE WHEN EXISTS(SELECT 1 FROM control_plane.ssh_execution_intents intent WHERE intent.invocation_id=invocation.id AND intent.organization_id=invocation.organization_id) THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
      safe_error_code=CASE WHEN EXISTS(SELECT 1 FROM control_plane.ssh_execution_intents intent WHERE intent.invocation_id=invocation.id AND intent.organization_id=invocation.organization_id) THEN 'INTEGRATION_OUTCOME_UNKNOWN' ELSE 'INTEGRATION_AUTHORIZATION_DENIED' END,
      lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
      version=invocation.version+1,updated_at=clock_timestamp()
  FROM control_plane.integration_connections connection,control_plane.ssh_invocation_identity_bindings pin
  WHERE invocation.connection_id=connection.id AND invocation.organization_id=@organization_id::uuid
    AND connection.organization_id=invocation.organization_id AND connection.definition_key IN('ssh-managed','ssh-execute')
    AND pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id AND pin.connection_id=connection.id
    AND invocation.state IN('READY','WAITING_APPROVAL','RUNNING') AND connection.ref=@connection_ref
    AND (@binding_id::text='' OR pin.binding_id=NULLIF(@binding_id,'')::uuid)
    AND (@identity_id::text='' OR pin.identity_revision_id=NULLIF(@identity_id,'')::uuid)
    AND (@grant_ref::text='' OR invocation.grant_id=(SELECT id FROM control_plane.integration_grants WHERE organization_id=@organization_id::uuid AND ref=@grant_ref))
  RETURNING invocation.id,invocation.ref,invocation.run_id,invocation.node_id,invocation.state
), gates AS (
  UPDATE control_plane.owner_gates gate
  SET state='CANCELLED',decision='CANCEL',decision_comment='i18n:INTEGRATION_ACTION_FAILED',
      resolved_by=@actor_id::uuid,resolved_at=clock_timestamp(),version=gate.version+1
  WHERE gate.organization_id=@organization_id::uuid AND gate.integration_invocation_id IN (SELECT id FROM closed) AND gate.state='OPEN'
  RETURNING gate.id,gate.ref,gate.node_id,gate.integration_invocation_id
), gate_nodes AS (
  UPDATE control_plane.run_nodes SET state='CANCELLED',finished_at=clock_timestamp(),version=version+1
  WHERE organization_id=@organization_id::uuid AND id IN (SELECT node_id FROM gates) AND state='WAITING'
), deliveries AS (
  UPDATE control_plane.interaction_deliveries
  SET state=CASE WHEN state='CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
      safe_error_code=CASE WHEN state='CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE safe_error_code END,
      lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
      completed_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp()
  WHERE organization_id=@organization_id::uuid AND gate_id IN (SELECT id FROM gates) AND state IN ('WAITING_APPROVAL','DUE','FAILED','CLAIMED')
), facts AS (
  SELECT 'INVOCATION'::text AS kind,closed.ref AS entity_ref,closed.id AS invocation_id,closed.node_id,closed.run_id,closed.state FROM closed
  UNION ALL
  SELECT 'GATE',gates.ref,gates.integration_invocation_id,gates.node_id,closed.run_id,'CANCELLED'
  FROM gates JOIN closed ON closed.id=gates.integration_invocation_id
)
SELECT facts.kind,facts.entity_ref,facts.invocation_id::text,node.ref,COALESCE(run.project_id::text,''),run.root_run_id::text,
       facts.state,root.state,node.state,organization.ref,actor.ref,actor.display_name
FROM facts JOIN control_plane.run_nodes node ON node.id=facts.node_id AND node.organization_id=@organization_id::uuid
JOIN control_plane.runs run ON run.id=facts.run_id AND run.organization_id=@organization_id::uuid
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.organizations organization ON organization.id=run.organization_id
JOIN control_plane.subjects actor ON actor.id=@actor_id::uuid AND actor.organization_id=organization.id
ORDER BY facts.kind,facts.entity_ref;
