-- name: artifacts_purge_delete_content :exec
DELETE FROM control_plane.artifact_revision_content content
USING control_plane.artifact_revisions revision
WHERE revision.id=content.revision_id AND revision.artifact_id=@artifact_id::uuid;
