-- name: kubernetes_execution__gate_origin :one
SELECT root.id::text,coalesce(root.project_id::text,''),current_run.id::text
FROM control_plane.integration_invocations invocation
JOIN control_plane.runs current_run ON current_run.id=invocation.run_id AND current_run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=current_run.root_run_id AND root.organization_id=invocation.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid;
