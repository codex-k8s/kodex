-- name: integration_pending_root__origin :one
SELECT root.id::text
FROM control_plane.runs execution
JOIN control_plane.runs root ON root.id=execution.root_run_id
 AND root.organization_id=execution.organization_id AND root.root_run_id=root.id
WHERE execution.organization_id=@organization_id::uuid AND execution.ref=@run_ref;
