-- name: integration_pending_invocation_receipt :one
SELECT intent_digest,response_payload,expires_at>clock_timestamp()
FROM control_plane.idempotency_receipts
WHERE organization_id=@organization_id::uuid AND actor_id=@actor_id::uuid
 AND operation=@operation AND idempotency_key=@key
FOR UPDATE;
