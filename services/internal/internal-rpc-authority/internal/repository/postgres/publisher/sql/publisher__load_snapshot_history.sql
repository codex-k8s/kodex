-- name: publisher__load_snapshot_history :many
SELECT
    source_revision,
    source_digest_sha256
FROM internal_rpc_authority.authority_snapshot_history
ORDER BY source_revision DESC
LIMIT 32;
