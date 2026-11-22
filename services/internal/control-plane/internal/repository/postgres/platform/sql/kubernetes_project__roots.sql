-- name: kubernetes_project__roots :many
SELECT root.id::text FROM control_plane.runs root
WHERE root.organization_id=@organization_id::uuid AND root.project_id=@project_id::uuid
 AND root.id=root.root_run_id
ORDER BY root.id;
