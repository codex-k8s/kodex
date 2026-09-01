-- name: artifacts_purge_update_idempotency_receipt :exec
UPDATE control_plane.idempotency_receipts
SET response_type = 'ARTIFACT',
    response_payload = @response_payload
WHERE organization_id = @organization_id::uuid
  AND actor_id = @actor_id::uuid
  AND operation = @operation
  AND idempotency_key = @idempotency_key
  AND intent_digest = @intent_digest;
