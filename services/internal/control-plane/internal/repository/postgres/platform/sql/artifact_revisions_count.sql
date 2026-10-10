-- name: artifact_revisions_count :one
SELECT count(*)
FROM control_plane.artifact_revisions revision
JOIN control_plane.artifact_heads artifact ON artifact.id=revision.artifact_id
WHERE artifact.organization_id=@organization_id::uuid AND artifact.ref=@artifact_ref
  AND artifact.lifecycle_state='ACTIVE';
