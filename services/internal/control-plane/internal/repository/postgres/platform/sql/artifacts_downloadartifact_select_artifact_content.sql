-- name: artifacts_downloadartifact_select_artifact_content :one
SELECT c.object_key, c.object_version, c.object_etag, c.digest, c.size_bytes
FROM control_plane.artifact_revision_content c
JOIN control_plane.artifact_history ar ON ar.revision_id=c.revision_id
WHERE ar.id = @artifact_id::uuid
  AND ar.organization_id = @organization_id::uuid
  AND ar.revision_id = @revision_id::uuid
  AND ar.scan_state = 'CLEAN'
  AND ar.lifecycle_state = 'ACTIVE';
