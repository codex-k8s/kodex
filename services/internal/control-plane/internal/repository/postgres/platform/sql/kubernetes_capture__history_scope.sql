-- name: kubernetes_capture__history_scope :one
SELECT root.id::text,root.ref,coalesce(project.ref,'')
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.run_id=run.id AND node.organization_id=run.organization_id
LEFT JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref AND node.ref=@node_ref
 AND root.initiated_by=@actor_id::uuid AND(@project_ref='' OR project.ref=@project_ref);
