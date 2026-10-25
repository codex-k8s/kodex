-- name: runtime_completeexecution_select_gate_policy :one
SELECT version.spec, node.workflow_step_key, agent.ref
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision
  ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
 AND revision.run_id=lease.run_id AND revision.node_id=lease.node_id
 AND revision.generation=lease.generation AND revision.input_digest=lease.input_digest
JOIN control_plane.run_nodes node
  ON node.id=lease.node_id AND node.organization_id=lease.organization_id
 AND node.run_id=revision.run_id AND node.root_run_id=revision.root_run_id
 AND node.agent_id=revision.agent_id AND node.attempt=revision.attempt
 AND node.turn_id=revision.turn_id
JOIN control_plane.runs run
  ON run.id=node.run_id AND run.organization_id=node.organization_id
 AND run.root_run_id=node.root_run_id AND run.session_id=revision.session_id
 AND run.project_id IS NOT DISTINCT FROM revision.project_id
JOIN control_plane.runs root
  ON root.id=node.root_run_id AND root.organization_id=node.organization_id
 AND root.root_run_id=root.id AND root.project_id IS NOT DISTINCT FROM run.project_id
 AND root.workflow_version_id=run.workflow_version_id
JOIN control_plane.workflow_versions version
  ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow
  ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
 AND workflow.project_id=root.project_id AND workflow.ref=root.target_ref
JOIN control_plane.agents agent
  ON agent.id=node.agent_id AND agent.organization_id=node.organization_id
 AND agent.project_id=root.project_id
WHERE lease.organization_id=@organization_id::uuid AND lease.id=@lease_id::uuid
  AND lease.runtime_revision_id=@runtime_revision_id::uuid
  AND lease.run_id=@run_id::uuid AND lease.node_id=@node_id::uuid
  AND revision.root_run_id=@root_run_id::uuid AND lease.generation=@generation
  AND root.target_type='WORKFLOW' AND node.type='AGENT_EXECUTION' AND node.human_gate_after
  AND revision.safe_snapshot->'promptSnapshot'->'contextPin'->>'WorkflowRef'=workflow.ref
  AND revision.safe_snapshot->'promptSnapshot'->'contextPin'->>'WorkflowRevisionRef'=version.ref
  AND revision.safe_snapshot->'promptSnapshot'->'contextPin'->>'WorkflowStageKey'=node.workflow_step_key
