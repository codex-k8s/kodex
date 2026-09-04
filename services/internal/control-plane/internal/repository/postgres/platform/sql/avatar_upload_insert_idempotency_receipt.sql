-- name: avatar_upload_insert_idempotency_receipt :exec
INSERT INTO control_plane.idempotency_receipts (
    organization_id, actor_id, operation, idempotency_key, intent_digest,
    response_type, response_payload, expires_at
)
VALUES (
    @organization_id::uuid, @actor_id::uuid, 'agent.avatar.upload',
    @idempotency_key, @intent_digest, 'AGENT', @response_payload,
    clock_timestamp() + interval '24 hours'
);
