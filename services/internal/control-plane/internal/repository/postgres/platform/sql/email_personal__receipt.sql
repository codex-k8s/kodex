-- name: email_personal__receipt :one
SELECT response_payload FROM control_plane.idempotency_receipts
WHERE organization_id=@organization_id::uuid AND actor_id=@actor_id::uuid
  AND operation=@operation AND idempotency_key=@idempotency_key
  AND expires_at>clock_timestamp();
