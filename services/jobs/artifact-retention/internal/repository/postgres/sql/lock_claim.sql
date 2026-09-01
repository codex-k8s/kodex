-- name: lock_claim :one
SELECT artifact.organization_id::text,
       COALESCE(artifact.project_id::text, ''),
       artifact.ref
FROM control_plane.artifacts AS artifact
WHERE artifact.id = @artifact_id::uuid
  AND artifact.lifecycle_state = 'PURGE_PENDING'
  AND artifact.retention_claim_owner = @claim_owner
  AND artifact.retention_claim_generation = @claim_generation
  AND artifact.retention_claim_expires_at > clock_timestamp()
FOR UPDATE;
