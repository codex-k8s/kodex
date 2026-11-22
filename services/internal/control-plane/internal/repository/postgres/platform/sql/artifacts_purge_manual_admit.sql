-- name: artifacts_purge_manual_admit :one
SELECT admitted, authorized_until
FROM control_plane.artifact_manual_purge_admit(
 @organization_id::uuid,@artifact_id::uuid,@actor_id::uuid,@idempotency_key,
 @intent_digest,@claim_owner,@claim_generation,@revision_id::uuid);
