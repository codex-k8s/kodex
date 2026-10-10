-- name: release_claim :exec
UPDATE control_plane.artifact_heads SET retention_claim_owner=NULL,retention_claim_expires_at=NULL
WHERE id=@artifact_id::uuid AND lifecycle_state='PURGE_PENDING'
  AND retention_claim_owner = @claim_owner AND retention_claim_generation = @claim_generation;
