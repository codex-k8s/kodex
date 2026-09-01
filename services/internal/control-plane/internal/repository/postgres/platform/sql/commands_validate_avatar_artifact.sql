-- name: commands_validate_avatar_artifact :one
SELECT artifact.ref
FROM control_plane.artifacts artifact
WHERE artifact.organization_id = @organization_id::uuid
  AND artifact.project_id = @project_id::uuid
  AND artifact.ref = @artifact_ref
  AND artifact.lifecycle_state = 'ACTIVE'
  AND artifact.scan_state = 'CLEAN'
  AND artifact.preview_state = 'AVAILABLE'
  AND artifact.media_type IN ('image/jpeg', 'image/png', 'image/webp')
  AND artifact.size_bytes BETWEEN 1 AND 5242880
FOR SHARE;
