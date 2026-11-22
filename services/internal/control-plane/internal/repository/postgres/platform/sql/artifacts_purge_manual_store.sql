-- name: artifacts_purge_manual_store :exec
INSERT INTO control_plane.idempotency_receipts
 (organization_id,actor_id,operation,idempotency_key,intent_digest,response_type,response_payload,expires_at)
VALUES(@organization_id::uuid,@actor_id::uuid,'artifact.purge',@idempotency_key,@intent_digest,
 'ARTIFACT',@response_payload,'infinity'::timestamptz);
