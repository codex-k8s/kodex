-- name: workflow_dispatch__completion :one
SELECT node.workflow_step_key,node.workflow_business_attempt,
       COALESCE(node.workflow_step_origin_node_id,node.id)::text
FROM control_plane.run_nodes node
JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.organization_id=node.organization_id
    AND revision.run_id=node.run_id AND revision.root_run_id=node.root_run_id AND revision.turn_id=node.turn_id
    AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=revision.organization_id
    AND lease.node_id=node.id AND lease.run_id=node.run_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
WHERE node.organization_id=@organization_id::uuid AND node.root_run_id=@root_run_id::uuid AND node.id=@node_id::uuid
    AND node.state IN ('SUCCEEDED','FAILED') AND node.workflow_business_attempt BETWEEN 1 AND 4
    AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid
    AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
    AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text;
