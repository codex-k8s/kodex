-- name: ordinary_integration_outcomes__source :one
SELECT invocation.ref,invocation.state,COALESCE(invocation.result_summary,''),COALESCE(invocation.safe_error_code,''),
       COALESCE(receipt.ref,''),gate.ref,gate.version,COALESCE(gate.decision,''),gate.decision_comment,revision.safe_snapshot->>'task'
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id AND node.run_id=lease.run_id
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=lease.organization_id
JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.session_id=session.id AND turn.run_id=run.id
JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=lease.organization_id AND project.lifecycle='ACTIVE'
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=lease.organization_id AND agent.project_id=project.id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=lease.organization_id AND actor.active AND actor.kind='USER'
JOIN control_plane.run_edges edge ON edge.organization_id=lease.organization_id AND edge.root_run_id=root.id
 AND edge.target_node_id=node.id AND edge.source_node_id=node.parent_node_id AND edge.type='CONTINUES'
JOIN control_plane.run_nodes source ON source.id=edge.source_node_id AND source.organization_id=lease.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.organization_id=lease.organization_id
 AND invocation.node_id=source.id AND invocation.run_id=run.id
JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id AND gate.organization_id=lease.organization_id
 AND gate.root_run_id=root.id AND gate.project_id=project.id AND gate.scope_kind='PROJECT'
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
 AND receipt.organization_id=invocation.organization_id AND receipt.invocation_id=invocation.id
 AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
 AND receipt.result_summary=invocation.result_summary
JOIN control_plane.catalog_access_targets project_target ON project_target.organization_id=lease.organization_id AND project_target.kind='PROJECT' AND project_target.id=project.id
JOIN control_plane.catalog_access_targets agent_target ON agent_target.organization_id=lease.organization_id AND agent_target.kind='AGENT' AND agent_target.id=agent.id
JOIN control_plane.catalog_access_targets run_target ON run_target.organization_id=lease.organization_id AND run_target.kind='RUN' AND run_target.id=root.id
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
 AND lease.fence_digest=@fence_digest AND lease.generation=@generation AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
 AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id)
 AND root.workflow_version_id IS NULL AND root.state IN ('RUNNING','WAITING_HUMAN') AND run.state='RUNNING'
 AND node.state='RUNNING' AND turn.state IN ('QUEUED','RUNNING')
 AND agent.enabled AND agent.state IN ('READY','RUNNING') AND agent.system_key IS NULL
 AND revision.run_id=run.id AND revision.node_id=node.id AND revision.root_run_id=root.id
 AND revision.session_id=session.id AND revision.turn_id=turn.id AND revision.agent_id=agent.id AND revision.project_id=project.id
 AND revision.generation=lease.generation AND revision.attempt=node.attempt AND revision.safe_snapshot->>'assistantScope'='NONE'
 AND revision.safe_snapshot->>'task'=turn.content
 AND source.agent_id=node.agent_id AND source.run_id=node.run_id AND source.root_run_id=root.id
 AND source.state='SUCCEEDED' AND source.finished_at IS NOT NULL AND source.attempt+1=node.attempt
 AND source.owner_rework_gate_id IS NULL AND node.owner_rework_gate_id IS NULL
 AND EXISTS(SELECT 1 FROM control_plane.runtime_leases source_lease
   JOIN control_plane.runtime_revisions source_revision ON source_revision.id=source_lease.runtime_revision_id
     AND source_revision.organization_id=source_lease.organization_id
   WHERE source_lease.organization_id=lease.organization_id AND source_lease.node_id=source.id AND source_lease.run_id=run.id
     AND source_lease.state='COMPLETED' AND source_revision.node_id=source.id AND source_revision.run_id=run.id
     AND source_revision.root_run_id=root.id AND source_revision.session_id=session.id AND source_revision.agent_id=agent.id
     AND source_revision.project_id=project.id AND source_revision.turn_id=source.turn_id
     AND source_revision.attempt=source.attempt AND source_revision.generation=source_lease.generation)
 AND invocation.ref=@source_invocation_ref
 AND ((gate.state='APPROVED' AND gate.decision='APPROVE' AND invocation.state IN ('SUCCEEDED','FAILED'))
   OR (gate.state='REJECTED' AND gate.decision='REJECT' AND invocation.state='REJECTED')
   OR (gate.state='CANCELLED' AND gate.decision='CANCEL' AND invocation.state='CANCELLED'))
 AND (invocation.state<>'SUCCEEDED' OR receipt.id IS NOT NULL)
 AND EXISTS(SELECT 1 FROM control_plane.memberships membership WHERE membership.organization_id=lease.organization_id
     AND membership.subject_id=actor.id AND membership.active)
 AND control_plane.catalog_resource_visible(project_target.organization_id,actor.id,'project.view','PROJECT',project_target.id,
     project_target.project_id,project_target.owner_id,project_target.related_ids,statement_timestamp(),false)
 AND control_plane.catalog_resource_visible(agent_target.organization_id,actor.id,'agent.view','AGENT',agent_target.id,
     agent_target.project_id,agent_target.owner_id,agent_target.related_ids,statement_timestamp(),false)
 AND control_plane.catalog_resource_visible(run_target.organization_id,actor.id,'run.view','RUN',run_target.id,
     run_target.project_id,run_target.owner_id,run_target.related_ids,statement_timestamp(),false)
FOR SHARE OF node,lease,root,actor;
