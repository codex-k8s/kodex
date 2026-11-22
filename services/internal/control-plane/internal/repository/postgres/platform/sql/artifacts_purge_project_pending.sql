-- name: artifacts_purge_project_pending :one
SELECT control_plane.artifact_manual_project_pending_count(@organization_id::uuid,@project_id::uuid);
