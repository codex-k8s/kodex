-- name: prompt_preview_attachment_items :many
SELECT artifact.ref
FROM control_plane.attachment_set_items item
JOIN control_plane.artifact_history artifact
  ON artifact.id = item.artifact_id
 AND artifact.organization_id = @organization_id::uuid
 AND artifact.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
 AND artifact.ref = item.artifact_ref AND artifact.revision = item.artifact_revision
 AND artifact.file_name = item.file_name AND artifact.media_type = item.media_type
 AND artifact.size_bytes = item.size_bytes AND artifact.digest = item.digest
 AND artifact.source = item.source
JOIN control_plane.artifact_revision_content content
  ON content.revision_id = artifact.revision_id AND content.digest = item.digest
 AND content.size_bytes = item.size_bytes
WHERE item.attachment_set_id = @attachment_set_id::uuid
  AND artifact.scan_state = 'CLEAN' AND artifact.lifecycle_state = 'ACTIVE'
ORDER BY item.position;
