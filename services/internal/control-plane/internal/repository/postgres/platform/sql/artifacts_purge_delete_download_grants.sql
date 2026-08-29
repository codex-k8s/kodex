-- name: artifacts_purge_delete_download_grants :exec
DELETE FROM control_plane.artifact_download_grants
WHERE artifact_id = @artifact_id::uuid;
