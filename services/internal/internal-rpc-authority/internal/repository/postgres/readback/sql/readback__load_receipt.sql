-- name: readback__load_receipt :one
SELECT
    receipt.receipt_id,
    receipt.challenge_id,
    receipt.evidence_jti,
    receipt.evidence_digest_sha256,
    receipt.semantic_request_digest_sha256,
    receipt.verifier_generation,
    receipt.accepted_at,
    receipt.expires_at
FROM internal_rpc_authority.authority_readback_attestation_receipts AS receipt
WHERE receipt.peer_spiffe_id = @peer_spiffe_id
  AND receipt.idempotency_key = @idempotency_key;
