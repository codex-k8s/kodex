-- name: project_purge_authority_drift_column :exec
GRANT UPDATE(backend_pid) ON control_plane.project_purge_context TO control_plane_runtime;
