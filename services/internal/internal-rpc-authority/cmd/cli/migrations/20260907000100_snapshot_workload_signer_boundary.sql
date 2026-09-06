-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- Watermark хранит поколение workload AUTHORIZATION_CONTEXT, а history —
-- независимое поколение manifest signer. Их числовое равенство не является
-- authority. Проверяем workload key в exact owner-published snapshot payload.
-- Прежние watermark, replay receipts и signer generations не переписываются.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION internal_rpc_authority.guard_snapshot_watermark() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE
    compact_snapshot text;
    encoded_payload text;
    payload jsonb;
    issuer_keys jsonb;
    issuer_count bigint;
    current_count bigint;
    matching_count bigint;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'authority snapshot deletion rejected' USING ERRCODE = '42501';
    END IF;
    IF NOT (internal_rpc_authority.workload_database_identity_allows_work(NEW.target_workload_id, 'ISSUER')
        OR internal_rpc_authority.workload_database_identity_allows_work(NEW.target_workload_id, 'VERIFIER'))
       OR NOT internal_rpc_authority.validate_snapshot_attestation_receipt(
           NEW.readback_attestation_receipt_id, NEW.target_workload_id,
           NEW.source_revision, NEW.source_digest_sha256) THEN
        RAISE EXCEPTION 'authority snapshot identity or attestation rejected' USING ERRCODE = '42501';
    END IF;
    SELECT history.snapshot_compact_jws INTO compact_snapshot
    FROM internal_rpc_authority.authority_snapshot_history AS history
    WHERE history.source_revision = NEW.source_revision
      AND history.source_digest_sha256 = NEW.source_digest_sha256
      AND history.key_set_revision = NEW.key_set_revision
      AND history.policy_revision = NEW.policy_revision;
    IF NOT FOUND OR compact_snapshot IS NULL
       OR octet_length(compact_snapshot) NOT BETWEEN 64 AND 1048576
       OR compact_snapshot !~ '^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$' THEN
        RAISE EXCEPTION 'authority snapshot published history rejected' USING ERRCODE = '42501';
    END IF;
    BEGIN
        encoded_payload := split_part(compact_snapshot, '.', 2);
        payload := convert_from(decode(
            rpad(translate(encoded_payload, '-_', '+/'),
                ((length(encoded_payload) + 3) / 4) * 4, '='),
            'base64'), 'UTF8')::jsonb;
        IF jsonb_typeof(payload->'issuers') IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'authority snapshot published payload rejected' USING ERRCODE = '42501';
        END IF;
        SELECT count(*) INTO issuer_count
        FROM jsonb_array_elements(payload->'issuers') AS issuer(value)
        WHERE issuer.value->>'workload_id' = NEW.target_workload_id;
        IF issuer_count <> 1 THEN
            RAISE EXCEPTION 'authority snapshot workload signer rejected' USING ERRCODE = '42501';
        END IF;
        SELECT issuer.value->'keys' INTO issuer_keys
        FROM jsonb_array_elements(payload->'issuers') AS issuer(value)
        WHERE issuer.value->>'workload_id' = NEW.target_workload_id;
        IF jsonb_typeof(issuer_keys) IS DISTINCT FROM 'array' THEN
            RAISE EXCEPTION 'authority snapshot published payload rejected' USING ERRCODE = '42501';
        END IF;
        SELECT count(*), count(*) FILTER (
            WHERE key.value->>'purpose' = 'AUTHORIZATION_CONTEXT'
              AND key.value->>'generation' = NEW.signer_generation::text
        ) INTO current_count, matching_count
        FROM jsonb_array_elements(issuer_keys) AS key(value)
        WHERE key.value->>'status' = 'CURRENT';
        IF current_count <> 1 OR matching_count <> 1 THEN
            RAISE EXCEPTION 'authority snapshot workload signer rejected' USING ERRCODE = '42501';
        END IF;
    EXCEPTION WHEN data_exception THEN
        -- Ошибка parser не допускает частично восстановленную модель.
        RAISE EXCEPTION 'authority snapshot published payload rejected' USING ERRCODE = '42501';
    END;
    IF TG_OP = 'UPDATE' AND (
        NEW.target_workload_id <> OLD.target_workload_id
        OR NEW.source_revision < OLD.source_revision
        OR NEW.key_set_revision < OLD.key_set_revision
        OR NEW.policy_revision < OLD.policy_revision
        OR NEW.signer_generation < OLD.signer_generation
        OR (NEW.source_revision = OLD.source_revision AND NEW.source_digest_sha256 <> OLD.source_digest_sha256)
    ) THEN
        RAISE EXCEPTION 'authority snapshot rollback rejected' USING ERRCODE = '42501';
    END IF;
    NEW.served_at := clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.guard_snapshot_watermark() FROM PUBLIC;
RESET ROLE;
