-- name: publisher__load_snapshot_history :many
SELECT
    source_revision,
    source_digest_sha256
FROM internal_rpc_authority.authority_snapshot_history
ORDER BY source_revision DESC
-- One extra row keeps an already persisted current revision available so the
-- publisher can remove it and rebuild the exact same 32-entry predecessor window.
LIMIT 33;
