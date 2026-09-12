-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_load_snapshot_predecessor(
    p_source_revision bigint,
    p_source_digest_sha256 text,
    p_registry_digest_sha256 text
) RETURNS TABLE (
    publication_intent_id uuid,
    publication_input_digest_sha256 text,
    source_revision bigint,
    source_digest_sha256 text,
    key_set_revision bigint,
    policy_revision bigint,
    signer_generation bigint,
    predecessor_revision bigint,
    predecessor_digest_sha256 text,
    snapshot_compact_jws text,
    published_at timestamp with time zone,
    registry_digest_sha256 text,
    rotation_phase text
)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')
       OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_registry_digest_sha256 !~ '^[a-f0-9]{64}$' THEN
        RETURN;
    END IF;
    RETURN QUERY
    SELECT
        history.publication_intent_id,
        history.publication_input_digest_sha256,
        history.source_revision,
        history.source_digest_sha256,
        history.key_set_revision,
        history.policy_revision,
        history.signer_generation,
        history.predecessor_revision,
        history.predecessor_digest_sha256,
        history.snapshot_compact_jws,
        history.published_at,
        intent.registry_source_digest_sha256,
        COALESCE(operation.phase, '')
    FROM internal_rpc_authority.authority_snapshot_history AS history
    JOIN internal_rpc_authority.authority_rotation_intents AS intent
      ON intent.intent_id = history.publication_intent_id
     AND intent.source_revision = history.source_revision
    LEFT JOIN internal_rpc_authority.authority_rotation_operation_phase_intents AS phase_intent
      ON phase_intent.publication_intent_id = history.publication_intent_id
    LEFT JOIN internal_rpc_authority.authority_rotation_operation_publications AS operation
      ON operation.operation_id = phase_intent.operation_id
     AND operation.phase = phase_intent.phase
    WHERE history.source_revision = p_source_revision
      AND history.source_digest_sha256 = p_source_digest_sha256
      AND intent.registry_source_digest_sha256 = p_registry_digest_sha256
      AND (
        (phase_intent.publication_intent_id IS NULL AND operation.publication_intent_id IS NULL) OR (
          phase_intent.publication_intent_id = history.publication_intent_id
          AND phase_intent.source_revision = history.source_revision
          AND phase_intent.registry_digest_sha256 = intent.registry_source_digest_sha256
          AND phase_intent.publication_input_digest_sha256 = history.publication_input_digest_sha256
          AND operation.publication_intent_id = history.publication_intent_id
          AND operation.source_revision = history.source_revision
          AND operation.registry_digest_sha256 = phase_intent.registry_digest_sha256
          AND operation.publication_input_digest_sha256 = history.publication_input_digest_sha256
          AND operation.snapshot_digest_sha256 = history.source_digest_sha256
        )
      );
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_load_snapshot_predecessor(bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_load_snapshot_predecessor(bigint,text,text) TO internal_rpc_authority_publisher;

RESET ROLE;

-- +goose Down
SET ROLE internal_rpc_authority_readback_owner;
DROP FUNCTION IF EXISTS internal_rpc_authority.publisher_load_snapshot_predecessor(bigint,text,text);
RESET ROLE;
