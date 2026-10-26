-- name: workflow_exception__history :one
SELECT root.workflow_exception_history FROM control_plane.runs root
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.root_run_id=root.id;
