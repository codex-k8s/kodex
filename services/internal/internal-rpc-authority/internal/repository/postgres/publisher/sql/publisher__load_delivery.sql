-- name: publisher__load_delivery :one
SELECT
    idempotency_key,
    directive_jti,
    directive_digest_sha256,
    delivery_receipt_compact_jws,
    role_credential_digest_sha256,
    credential_generation,
    ack_key_generation,
    accepted_at
FROM internal_rpc_authority.authority_publisher_delivery_receipts
WHERE idempotency_key = @idempotency_key;
