-- name: workflow_upstream_proof :one
SELECT root.workflow_exception_history,
       control_plane.workflow_rework_floors(root.organization_id,root.id),
       COALESCE((SELECT jsonb_object_agg(node.ref,gate.ref)
           FROM control_plane.run_nodes node JOIN control_plane.owner_gates gate ON gate.id=node.workflow_rework_gate_id
           WHERE node.organization_id=root.organization_id AND node.root_run_id=root.id),'{}'::jsonb)
FROM control_plane.runs root JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE root.ref=@run_ref AND project.ref=@project_ref AND root.id=root.root_run_id;
