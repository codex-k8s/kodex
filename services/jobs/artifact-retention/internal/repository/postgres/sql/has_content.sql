-- name: has_content :one
SELECT EXISTS (SELECT 1 FROM control_plane.artifact_revisions revision
    JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
    WHERE revision.artifact_id=@artifact_id::uuid);
