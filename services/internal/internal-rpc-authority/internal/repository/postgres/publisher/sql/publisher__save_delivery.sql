-- name: publisher__save_delivery :one
INSERT INTO internal_rpc_authority.authority_publisher_delivery_receipts (
    idempotency_key,
    directive_jti,
    directive_digest_sha256,
    delivery_receipt_compact_jws,
    role_credential_digest_sha256,
    credential_generation,
    ack_key_generation,
    accepted_at
)
VALUES (
    @idempotency_key,
    @directive_jti,
    @directive_digest_sha256,
    @delivery_receipt_compact_jws,
    @role_credential_digest_sha256,
    @credential_generation,
    @ack_key_generation,
    @accepted_at
)
ON CONFLICT (idempotency_key) DO UPDATE
SET directive_digest_sha256 =
        internal_rpc_authority.authority_publisher_delivery_receipts.directive_digest_sha256
WHERE internal_rpc_authority.authority_publisher_delivery_receipts.directive_digest_sha256 =
        EXCLUDED.directive_digest_sha256
  AND internal_rpc_authority.authority_publisher_delivery_receipts.directive_jti =
        EXCLUDED.directive_jti
RETURNING
    idempotency_key,
    directive_jti,
    directive_digest_sha256,
    delivery_receipt_compact_jws,
    role_credential_digest_sha256,
    credential_generation,
    ack_key_generation,
    accepted_at;
