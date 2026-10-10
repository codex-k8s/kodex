-- name: delete_content :exec
DELETE FROM control_plane.artifact_revision_content content
USING control_plane.artifact_revisions revision
WHERE content.revision_id=revision.id AND revision.artifact_id=@artifact_id::uuid
  AND content.revision_id=@revision_id::uuid
  AND content.object_key=@object_key AND content.object_version=@object_version;
