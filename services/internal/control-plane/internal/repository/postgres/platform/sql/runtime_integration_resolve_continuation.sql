-- name: runtime_integration_resolve_continuation :one
SELECT node.id::text,
       node.run_id::text,
       node.root_run_id::text,
       node.agent_id::text,
       node.attempt,
       node.display_name,
       node.role,
       run.session_id::text,
       agent.ref,
       COALESCE(root.workflow_version_id::text,''),
       invocation.ref,
       invocation.state,
       COALESCE(invocation.result_summary,''),
       COALESCE(invocation.safe_error_code,'')
FROM control_plane.integration_invocations invocation
JOIN control_plane.run_nodes node ON node.id=invocation.node_id
JOIN control_plane.runs run ON run.id=node.run_id
JOIN control_plane.runs root ON root.id=node.root_run_id
JOIN control_plane.agents agent ON agent.id=node.agent_id
JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id
WHERE invocation.id=@invocation_id::uuid
  AND invocation.organization_id=@organization_id::uuid
  AND invocation.run_id=run.id
  AND node.organization_id=invocation.organization_id
  AND run.organization_id=invocation.organization_id
  AND root.organization_id=invocation.organization_id
  AND gate.organization_id=invocation.organization_id
  AND gate.project_id IS NOT DISTINCT FROM run.project_id
  AND (gate.scope_kind='PROJECT' OR (gate.scope_kind='ORGANIZATION'
       AND control_plane.owned_organization_assistant_run(gate.organization_id,root.id)))
  AND gate.root_run_id=root.id
  AND node.state='SUCCEEDED'
  AND node.finished_at IS NOT NULL
  AND invocation.updated_at>node.finished_at
  AND root.state IN ('RUNNING','WAITING_HUMAN')
  AND ((gate.state='APPROVED' AND invocation.state IN ('SUCCEEDED','FAILED'))
    OR (gate.state='REJECTED' AND invocation.state='REJECTED')
    OR (gate.state='CANCELLED' AND invocation.state='CANCELLED'))
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.run_edges continuation
    WHERE continuation.root_run_id=root.id
      AND continuation.source_node_id=node.id
      AND continuation.type='CONTINUES'
  )
FOR UPDATE OF node,root
