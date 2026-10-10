-- name: artifacts_purge_finalize :exec
UPDATE control_plane.artifact_heads
SET lifecycle_state = 'PURGED',
    current_revision_id = NULL,
    purged_at = clock_timestamp(),
    version = version + 1
WHERE id = @artifact_id::uuid
  AND lifecycle_state = 'PURGE_PENDING'
  AND NOT control_plane.artifact_has_retained_revisions(id)
  AND NOT EXISTS (SELECT 1 FROM control_plane.artifact_revisions revision
      JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
      WHERE revision.artifact_id=artifact_heads.id);
