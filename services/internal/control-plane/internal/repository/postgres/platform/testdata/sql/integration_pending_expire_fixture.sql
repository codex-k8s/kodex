-- name: integration_pending_expire_fixture :exec
UPDATE control_plane.idempotency_receipts
SET expires_at=clock_timestamp()-interval '1 second'
WHERE operation=@operation AND convert_from(response_payload,'UTF8')::jsonb->>'invocationRef'=@invocation_ref;
