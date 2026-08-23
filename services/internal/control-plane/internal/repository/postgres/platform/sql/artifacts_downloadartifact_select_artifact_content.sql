-- name: artifacts_downloadartifact_select_artifact_content :one
SELECT c.body
FROM control_plane.artifact_content c
JOIN control_plane.artifacts ar ON ar.id = c.artifact_id
WHERE ar.id = @artifact_id::uuid
  AND ar.organization_id = @organization_id::uuid
  AND ar.version = @artifact_version
  AND ar.scan_state = 'CLEAN';
