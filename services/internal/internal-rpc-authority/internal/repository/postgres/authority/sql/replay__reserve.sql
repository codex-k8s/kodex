-- name: replay__reserve :one
INSERT INTO internal_rpc_authority.replay_reservations (
    reservation_kind,
    jti,
    canonical_digest_sha256,
    expires_at
)
VALUES (
    @reservation_kind,
    @jti,
    @canonical_digest_sha256,
    @expires_at
)
ON CONFLICT (reservation_kind, jti) DO NOTHING
RETURNING true;
