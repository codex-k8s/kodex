-- name: workflow_exception__manager_origin :one
SELECT EXISTS(SELECT 1 FROM control_plane.run_nodes node
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=root.organization_id
    AND agent.project_id=root.project_id AND agent.ref=version.spec->>'CoordinatorAgentRef'
JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.organization_id=node.organization_id
    AND revision.root_run_id=node.root_run_id AND revision.run_id=node.run_id AND revision.turn_id=node.turn_id
    AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=revision.organization_id
    AND lease.node_id=node.id AND lease.run_id=node.run_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
WHERE node.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND node.id=@node_id::uuid
    AND node.workflow_business_attempt=0 AND node.workflow_step_key='workflow.coordinator.exception.'||@episode::bigint
    AND node.state IN ('SUCCEEDED','FAILED') AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid);
