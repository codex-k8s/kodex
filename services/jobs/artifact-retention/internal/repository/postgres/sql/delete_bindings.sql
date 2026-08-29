-- name: artifact_retention_delete_bindings :exec
DELETE FROM control_plane.artifact_bindings
WHERE artifact_id = @artifact_id::uuid;
