-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- Исправленная связь добавляется отдельно: опубликованные history/intent не меняются.
CREATE TABLE internal_rpc_authority.authority_legacy_registry_provenance (
    publication_intent_id uuid PRIMARY KEY REFERENCES internal_rpc_authority.authority_rotation_intents(intent_id),
    source_revision bigint NOT NULL UNIQUE,
    snapshot_digest_sha256 text NOT NULL CHECK (snapshot_digest_sha256 ~ '^[a-f0-9]{64}$'),
    input_digest_sha256 text NOT NULL CHECK (input_digest_sha256 ~ '^[a-f0-9]{64}$'),
    registry_digest_sha256 text NOT NULL CHECK (registry_digest_sha256 ~ '^[a-f0-9]{64}$'),
    repaired_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    repaired_by name NOT NULL DEFAULT session_user
);
ALTER TABLE internal_rpc_authority.authority_legacy_registry_provenance ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_legacy_registry_provenance FORCE ROW LEVEL SECURITY;
CREATE POLICY authority_legacy_registry_provenance_owner ON internal_rpc_authority.authority_legacy_registry_provenance
    TO internal_rpc_authority_readback_owner USING (true) WITH CHECK (true);

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.repair_legacy_registry_provenance(
    p_source_revision bigint, p_snapshot_digest_sha256 text, p_input_preimage text
) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER
SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    proof jsonb;
    input_digest text;
    registry_digest text;
    publication uuid;
BEGIN
    IF NOT pg_has_role(session_user, 'internal_rpc_authority_migrator', 'MEMBER')
       OR p_source_revision IS NULL OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_snapshot_digest_sha256 IS NULL OR p_snapshot_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_input_preimage IS NULL OR octet_length(p_input_preimage) NOT BETWEEN 10 AND 524288 THEN
        RETURN false;
    END IF;
    BEGIN
        proof := p_input_preimage::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN
        RETURN false;
    END;
    IF jsonb_typeof(proof) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
    IF (SELECT count(*) FROM jsonb_object_keys(proof)) <> 3
       OR jsonb_typeof(proof->'manifest_bundle') IS DISTINCT FROM 'string'
       OR jsonb_typeof(proof->'policy') IS DISTINCT FROM 'string'
       OR jsonb_typeof(proof->'registry_digest_sha256') IS DISTINCT FROM 'string'
       OR length(proof->>'manifest_bundle') = 0 OR length(proof->>'policy') = 0
       OR proof->>'registry_digest_sha256' !~ '^[a-f0-9]{64}$' THEN
        RETURN false;
    END IF;
    input_digest := encode(sha256(convert_to(p_input_preimage, 'UTF8')), 'hex');
    registry_digest := proof->>'registry_digest_sha256';
    -- Преобразование текста не используется: проверяем точный preimage исторического hash.
    PERFORM pg_advisory_xact_lock(1390, 1465);
    SELECT h.publication_intent_id INTO publication
      FROM internal_rpc_authority.authority_snapshot_history h
      JOIN internal_rpc_authority.authority_rotation_intents i ON i.intent_id=h.publication_intent_id
       AND i.source_revision=h.source_revision
     WHERE h.source_revision=p_source_revision AND h.source_digest_sha256=p_snapshot_digest_sha256
       AND h.publication_input_digest_sha256=input_digest AND i.protocol_version=1
       AND i.status IN ('PROMOTED','RETIRED')
       AND i.source_digest_sha256=h.source_digest_sha256
       AND i.registry_source_digest_sha256=h.source_digest_sha256;
    IF NOT FOUND THEN RETURN false; END IF;
    INSERT INTO internal_rpc_authority.authority_legacy_registry_provenance
      (publication_intent_id,source_revision,snapshot_digest_sha256,input_digest_sha256,registry_digest_sha256)
      VALUES (publication,p_source_revision,p_snapshot_digest_sha256,input_digest,registry_digest)
      ON CONFLICT DO NOTHING;
    RETURN EXISTS(SELECT 1 FROM internal_rpc_authority.authority_legacy_registry_provenance p
      WHERE p.publication_intent_id=publication AND p.source_revision=p_source_revision
        AND p.snapshot_digest_sha256=p_snapshot_digest_sha256 AND p.input_digest_sha256=input_digest
        AND p.registry_digest_sha256=registry_digest);
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.repair_legacy_registry_provenance(bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.repair_legacy_registry_provenance(bigint,text,text) TO internal_rpc_authority_migrator;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION internal_rpc_authority.publisher_load_snapshot_predecessor(
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
        COALESCE(legacy.registry_digest_sha256, intent.registry_source_digest_sha256),
        COALESCE(operation.phase, '')
    FROM internal_rpc_authority.authority_snapshot_history AS history
    JOIN internal_rpc_authority.authority_rotation_intents AS intent
      ON intent.intent_id = history.publication_intent_id
     AND intent.source_revision = history.source_revision
    LEFT JOIN internal_rpc_authority.authority_legacy_registry_provenance AS legacy
      ON intent.protocol_version=1 AND intent.registry_source_digest_sha256=history.source_digest_sha256
     AND legacy.publication_intent_id=history.publication_intent_id
     AND legacy.source_revision=history.source_revision
     AND legacy.snapshot_digest_sha256=history.source_digest_sha256
     AND legacy.input_digest_sha256=history.publication_input_digest_sha256
    LEFT JOIN internal_rpc_authority.authority_rotation_operation_phase_intents AS phase_intent
      ON phase_intent.publication_intent_id = history.publication_intent_id
    LEFT JOIN internal_rpc_authority.authority_rotation_operation_publications AS operation
      ON operation.operation_id = phase_intent.operation_id
     AND operation.phase = phase_intent.phase
    WHERE history.source_revision = p_source_revision
      AND history.source_digest_sha256 = p_source_digest_sha256
      AND COALESCE(legacy.registry_digest_sha256, intent.registry_source_digest_sha256) = p_registry_digest_sha256
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
-- Forward-only: repair evidence и опубликованные границы сохраняются.
