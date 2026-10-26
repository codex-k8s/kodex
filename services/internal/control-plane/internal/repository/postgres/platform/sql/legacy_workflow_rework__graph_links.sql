-- name: legacy_workflow_rework__graph_links :many
SELECT edge.ref,prior.ref,fresh.ref,predecessor.ref,rework.ref
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
    AND workflow.project_id=root.project_id
JOIN control_plane.run_nodes rework ON rework.organization_id=root.organization_id AND rework.root_run_id=root.id
    AND rework.owner_rework_gate_id IS NOT NULL AND rework.owner_rework_origin_node_id IS NULL
JOIN control_plane.owner_gates decision ON decision.id=rework.owner_rework_gate_id AND decision.organization_id=root.organization_id
    AND decision.root_run_id=root.id AND decision.project_id=root.project_id
    AND decision.state='CHANGES_REQUESTED' AND decision.decision='REQUEST_CHANGES'
JOIN control_plane.run_nodes gate_node ON gate_node.id=decision.node_id AND gate_node.organization_id=root.organization_id
    AND gate_node.root_run_id=root.id AND gate_node.type='HUMAN_GATE'
JOIN control_plane.run_nodes predecessor ON predecessor.id=gate_node.parent_node_id AND predecessor.organization_id=root.organization_id
    AND predecessor.root_run_id=root.id AND predecessor.agent_id=rework.agent_id
    AND predecessor.state='SUCCEEDED'
JOIN LATERAL control_plane.legacy_workflow_step_origin(root.organization_id,predecessor.id) origin_id ON true
JOIN control_plane.run_nodes logical_origin ON logical_origin.id=origin_id AND logical_origin.workflow_step_key=rework.workflow_step_key
JOIN control_plane.runs prior ON prior.id=predecessor.run_id AND prior.organization_id=root.organization_id
    AND prior.root_run_id=root.id AND prior.project_id=root.project_id AND prior.state='SUCCEEDED'
JOIN control_plane.runs fresh ON fresh.id=rework.run_id AND fresh.organization_id=root.organization_id
    AND fresh.root_run_id=root.id AND fresh.project_id=root.project_id AND fresh.retry_of_run_id=prior.id
    AND fresh.attempt=prior.attempt+1 AND fresh.workflow_version_id=root.workflow_version_id
    AND fresh.initiated_by=root.initiated_by AND prior.initiated_by=root.initiated_by
JOIN control_plane.run_edges edge ON edge.organization_id=root.organization_id AND edge.root_run_id=root.id
    AND edge.source_node_id=predecessor.id AND edge.target_node_id=rework.id AND edge.type='RETRY_OF'
JOIN control_plane.agents agent ON agent.id=rework.agent_id AND agent.organization_id=root.organization_id AND agent.project_id=root.project_id
WHERE root.organization_id=@organization_id::uuid AND root.ref=@root_ref AND root.root_run_id=root.id
    AND COALESCE(version.spec->>'ExecutionMode','')<>'SERVER_DAG_V1'
    AND EXISTS (SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
        WHERE step.value->>'Key'=rework.workflow_step_key AND step.value->>'AgentRef'=agent.ref);
