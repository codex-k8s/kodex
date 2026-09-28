-- name: runtime_integration_read_root_state :one
SELECT state FROM control_plane.runs
WHERE id=@root_run_id::uuid AND organization_id=@organization_id::uuid
