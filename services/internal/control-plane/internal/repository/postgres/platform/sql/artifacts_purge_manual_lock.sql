-- name: artifacts_purge_manual_lock :one
SELECT 1 FROM control_plane.artifact_heads
WHERE id=@artifact_id::uuid AND organization_id=@organization_id::uuid
 AND lifecycle_state='PURGE_PENDING' AND manual_purge_actor_id=@actor_id::uuid
 AND manual_purge_key=@idempotency_key AND retention_claim_owner=@claim_owner
 AND retention_claim_generation=@claim_generation AND retention_claim_expires_at>clock_timestamp()
 AND NOT control_plane.artifact_has_retained_revisions(id)
FOR UPDATE;
