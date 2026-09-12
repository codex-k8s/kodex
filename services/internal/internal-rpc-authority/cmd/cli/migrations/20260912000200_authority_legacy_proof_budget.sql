-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- Bounded proof envelope допускает действующую policy размером более512KiB.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION internal_rpc_authority.repair_legacy_registry_provenance(
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
       OR p_input_preimage IS NULL OR octet_length(p_input_preimage) NOT BETWEEN 10 AND 1048576 THEN
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
RESET ROLE;

-- +goose Down
-- Forward-only: сохраняем предел1MiB и прежние доказательства.
