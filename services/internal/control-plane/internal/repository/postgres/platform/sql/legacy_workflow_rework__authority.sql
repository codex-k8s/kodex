-- name: legacy_workflow_rework__authority :one
SELECT root.target_ref,root.project_id::text,project.ref,actor.ref,organization.ref
FROM control_plane.run_nodes node
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    AND root.target_type='WORKFLOW' AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.projects project ON project.id=root.project_id AND project.lifecycle='ACTIVE'
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id
JOIN control_plane.organizations organization ON organization.id=root.organization_id
WHERE node.id=@node_id::uuid AND node.organization_id=@organization_id::uuid AND node.owner_rework_gate_id IS NOT NULL;
