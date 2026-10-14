-- name: artifacts_purge_delete_revisions :exec
DELETE FROM control_plane.artifact_revisions WHERE artifact_id=@artifact_id::uuid;
