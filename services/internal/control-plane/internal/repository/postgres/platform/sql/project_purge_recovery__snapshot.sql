-- name: project_purge_recovery__snapshot :one
SELECT control_plane.project_purge_recovery_snapshot(@organization_id::uuid,@project_ref,@storage_identity);
