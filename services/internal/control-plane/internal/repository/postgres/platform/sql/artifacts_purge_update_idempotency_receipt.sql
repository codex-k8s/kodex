-- name: artifacts_purge_update_idempotency_receipt :exec
UPDATE control_plane.idempotency_receipts
SET stored_result = @stored_result::jsonb
WHERE organization_id = @organization_id::uuid
  AND actor_id = @actor_id::uuid
  AND operation = @operation
  AND idempotency_key = @idempotency_key
  AND intent_digest = @intent_digest;
