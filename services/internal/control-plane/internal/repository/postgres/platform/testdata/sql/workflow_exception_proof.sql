-- name: workflow_exception_proof :one
SELECT root.id::text,root.state,root.workflow_exception_history,
       (SELECT count(*) FROM control_plane.run_nodes node WHERE node.organization_id=root.organization_id AND node.root_run_id=root.id
            AND node.workflow_step_key LIKE 'workflow.coordinator.exception.%'),
       (SELECT count(*) FROM control_plane.runtime_leases lease JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id
            WHERE node.root_run_id=root.id AND lease.state='CLAIMED'),
       (SELECT node.run_id::text FROM control_plane.run_nodes node WHERE node.organization_id=root.organization_id AND node.root_run_id=root.id
            AND node.workflow_step_key='work' AND node.workflow_step_origin_node_id IS NULL)
FROM control_plane.runs root
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE root.ref=@run_ref AND project.ref=@project_ref AND root.id=root.root_run_id;
