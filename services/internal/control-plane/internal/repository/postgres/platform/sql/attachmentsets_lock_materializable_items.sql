-- name: attachmentsets_lock_materializable_items :many
SELECT artifact.id::text
FROM control_plane.attachment_set_items AS item
JOIN control_plane.artifact_history AS artifact
  ON artifact.id = item.artifact_id
 AND artifact.organization_id = @organization_id::uuid
 AND artifact.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
 AND artifact.ref = item.artifact_ref
 AND artifact.revision = item.artifact_revision
 AND artifact.file_name = item.file_name
 AND artifact.media_type = item.media_type
 AND artifact.size_bytes = item.size_bytes
 AND artifact.digest = item.digest
 AND artifact.source = item.source
JOIN control_plane.artifact_revision_content AS content
  ON content.revision_id = artifact.revision_id
 AND content.digest = item.digest
 AND content.size_bytes = item.size_bytes
JOIN control_plane.artifact_heads head ON head.id=artifact.id
WHERE item.attachment_set_id = @attachment_set_id::uuid
  AND artifact.scan_state = 'CLEAN'
  AND (
      artifact.lifecycle_state = 'ACTIVE'
      OR (@allow_soft_deleted::boolean AND artifact.lifecycle_state = 'DELETED')
  )
ORDER BY artifact.id
FOR SHARE OF head;
