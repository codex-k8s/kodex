-- name: provider_financial_receipt :one
-- Только после fresh execution/current credential/deadline guard; не общий replay.
SELECT intent_digest, response_payload
FROM control_plane.idempotency_receipts
WHERE organization_id = $1::uuid AND actor_id = $2::uuid
  AND operation = $3 AND idempotency_key = $4
FOR UPDATE;
