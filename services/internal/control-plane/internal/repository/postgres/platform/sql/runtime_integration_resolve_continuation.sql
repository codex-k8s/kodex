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
       COALESCE(invocation.safe_error_code,''),
       gate.ref,
       gate.version,
       COALESCE(gate.decision,''),
       gate.decision_comment,
       COALESCE(receipt.ref,'')
FROM control_plane.integration_invocations invocation
JOIN control_plane.run_nodes node ON node.id=invocation.node_id
JOIN control_plane.runs run ON run.id=node.run_id
JOIN control_plane.runs root ON root.id=node.root_run_id
JOIN control_plane.agents agent ON agent.id=node.agent_id
JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
  AND receipt.invocation_id=invocation.id AND receipt.organization_id=invocation.organization_id
  AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
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
  AND (invocation.updated_at>node.finished_at OR invocation.operation='github.repository.bundle.publish'
       OR node.owner_rework_gate_id IS NOT NULL)
  AND (node.owner_rework_gate_id IS NULL OR (
    invocation.risk='READ' AND control_plane.legacy_rework_execution_assigned(node.organization_id,node.id)
    AND EXISTS (SELECT 1 FROM control_plane.subjects initiator
      JOIN control_plane.workflow_versions pinned ON pinned.id=root.workflow_version_id AND pinned.organization_id=root.organization_id
      JOIN control_plane.workflows workflow ON workflow.id=pinned.workflow_id AND workflow.organization_id=root.organization_id
        AND workflow.project_id=root.project_id AND workflow.ref=root.target_ref
      JOIN control_plane.catalog_access_targets target ON target.organization_id=root.organization_id AND target.kind='WORKFLOW' AND target.id=workflow.id
      WHERE initiator.id=root.initiated_by AND initiator.organization_id=root.organization_id AND initiator.active
        AND control_plane.catalog_resource_visible(root.organization_id,initiator.id,'workflow.launch','WORKFLOW',target.id,
          target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false))
  ))
  AND root.state IN ('RUNNING','WAITING_HUMAN')
  AND (invocation.operation<>'github.repository.bundle.publish' OR (
    agent.enabled AND agent.state='READY'
    AND EXISTS (SELECT 1 FROM control_plane.projects project WHERE project.id=run.project_id AND project.lifecycle='ACTIVE')
    AND EXISTS (SELECT 1 FROM control_plane.subjects initiator
      JOIN control_plane.catalog_access_targets target ON target.organization_id=initiator.organization_id AND target.kind='AGENT' AND target.id=agent.id
      WHERE initiator.id=root.initiated_by AND initiator.organization_id=invocation.organization_id AND initiator.active
        AND control_plane.catalog_resource_visible(invocation.organization_id,initiator.id,'agent.launch','AGENT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false))
  ))
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
