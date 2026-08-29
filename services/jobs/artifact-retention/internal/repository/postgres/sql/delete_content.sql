-- name: artifact_retention_delete_content :exec
DELETE FROM control_plane.artifact_content
WHERE artifact_id = @artifact_id::uuid;
