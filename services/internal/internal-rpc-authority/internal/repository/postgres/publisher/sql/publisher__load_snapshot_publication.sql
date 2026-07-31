-- name: publisher__load_snapshot_publication :one
SELECT
    publication_intent_id,
    publication_input_digest_sha256,
    source_revision,
    source_digest_sha256,
    key_set_revision,
    policy_revision,
    signer_generation,
    predecessor_revision,
    predecessor_digest_sha256,
    snapshot_compact_jws,
    published_at
FROM internal_rpc_authority.authority_snapshot_history
WHERE source_revision = @source_revision
  AND publication_input_digest_sha256 = @publication_input_digest_sha256;
