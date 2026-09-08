-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- Additive-фаза сохраняет старым binaries 300s до отдельного CAS activation.
-- Новый reader helper всегда применяет accepted_at+30s, без renewal при replay.
CREATE TABLE internal_rpc_authority.authority_freshness_policy (
    singleton boolean PRIMARY KEY CHECK (singleton),
    version bigint NOT NULL CHECK (version IN (1, 2)),
    maximum_age_seconds integer NOT NULL CHECK (maximum_age_seconds IN (30, 300)),
    activation_id uuid UNIQUE,
    activated_at timestamptz,
    CHECK ((version = 1 AND maximum_age_seconds = 300 AND activation_id IS NULL AND activated_at IS NULL)
        OR (version = 2 AND maximum_age_seconds = 30 AND activation_id IS NOT NULL AND activated_at IS NOT NULL))
);
INSERT INTO internal_rpc_authority.authority_freshness_policy
    (singleton, version, maximum_age_seconds) VALUES (true, 1, 300);
REVOKE ALL ON internal_rpc_authority.authority_freshness_policy FROM PUBLIC;

-- Только migrator после проверенного rollout всех readers включает строгую
-- границу. Runtime issuer/verifier/attestor не могут её ослаблять/активировать.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.activate_authority_freshness(
    p_expected_version bigint, p_activation_id uuid
) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE
    current_policy internal_rpc_authority.authority_freshness_policy%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'internal_rpc_authority_migrator', 'MEMBER')
       OR p_activation_id IS NULL OR p_expected_version IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'freshness activation identity rejected' USING ERRCODE = '42501';
    END IF;
    SELECT * INTO STRICT current_policy
    FROM internal_rpc_authority.authority_freshness_policy
    WHERE singleton FOR UPDATE;
    IF current_policy.version = 2 AND current_policy.activation_id = p_activation_id THEN
        RETURN current_policy.version;
    END IF;
    IF current_policy.version <> p_expected_version THEN
        RAISE EXCEPTION 'freshness activation version conflict' USING ERRCODE = '40001';
    END IF;
    UPDATE internal_rpc_authority.authority_freshness_policy
    SET version = 2, maximum_age_seconds = 30,
        activation_id = p_activation_id, activated_at = clock_timestamp()
    WHERE singleton;
    RETURN 2;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.activate_authority_freshness(bigint, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.activate_authority_freshness(bigint, uuid)
    TO internal_rpc_authority_migrator;

-- Readback показывает только policy metadata, без credential или payload.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.authority_freshness_status()
RETURNS TABLE (version bigint, maximum_age_seconds integer, activation_id uuid, activated_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
BEGIN
    IF NOT pg_has_role(session_user, 'internal_rpc_authority_migrator', 'MEMBER') THEN
        RAISE EXCEPTION 'freshness readback identity rejected' USING ERRCODE = '42501';
    END IF;
    RETURN QUERY SELECT p.version, p.maximum_age_seconds, p.activation_id, p.activated_at
        FROM internal_rpc_authority.authority_freshness_policy AS p WHERE p.singleton;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.authority_freshness_status() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.authority_freshness_status()
    TO internal_rpc_authority_migrator;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION "internal_rpc_authority"."consume_authority_readback_attestation_challenge"("p_challenge_id" "uuid", "p_receipt_id" "uuid", "p_evidence_jti" "uuid", "p_evidence_digest_sha256" "text", "p_verifier_generation" bigint, "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") RETURNS "uuid"
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    existing internal_rpc_authority.authority_readback_attestation_receipts%ROWTYPE;
    challenge internal_rpc_authority.authority_readback_attestation_challenges%ROWTYPE;
    intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
    challenge_peer_spiffe_id text;
    accepted_at timestamptz;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_readback_attestor',
        'MEMBER'
    )
       OR p_evidence_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_semantic_request_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_verifier_generation NOT BETWEEN 1 AND 9007199254740991
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RAISE EXCEPTION 'readback receipt binding rejected';
    END IF;

    SELECT peer_spiffe_id
    INTO challenge_peer_spiffe_id
    FROM internal_rpc_authority.authority_readback_attestation_challenges
    WHERE challenge_id = p_challenge_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'readback challenge replay or expiry rejected';
    END IF;

    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            'internal_rpc_authority.readback_receipt:' ||
                challenge_peer_spiffe_id || ':' || p_idempotency_key::text,
            0
        )
    );
    accepted_at := pg_catalog.clock_timestamp();

    SELECT *
    INTO existing
    FROM internal_rpc_authority.authority_readback_attestation_receipts
    WHERE peer_spiffe_id = challenge_peer_spiffe_id
      AND idempotency_key = p_idempotency_key;
    IF FOUND THEN
        IF existing.challenge_id <> p_challenge_id
           OR existing.evidence_jti <> p_evidence_jti
           OR existing.evidence_digest_sha256 <> p_evidence_digest_sha256
           OR existing.semantic_request_digest_sha256 <>
                p_semantic_request_digest_sha256
        THEN
            RAISE EXCEPTION 'readback receipt idempotency conflict';
        END IF;
        RETURN existing.receipt_id;
    END IF;

    SELECT *
    INTO challenge
    FROM internal_rpc_authority.authority_readback_attestation_challenges
    WHERE challenge_id = p_challenge_id
    FOR UPDATE;
    IF NOT FOUND
       OR challenge.peer_spiffe_id <> challenge_peer_spiffe_id
       OR challenge.consumed_at IS NOT NULL
       OR challenge.expires_at < accepted_at
    THEN
        RAISE EXCEPTION 'readback challenge replay or expiry rejected';
    END IF;
    SELECT *
    INTO intent
    FROM internal_rpc_authority.authority_readback_intents
    WHERE intent_id = challenge.intent_id
    FOR UPDATE;
    IF NOT FOUND
       OR intent.status <> 'PINNED'
       OR intent.expires_at < accepted_at
    THEN
        RAISE EXCEPTION 'readback intent rejected';
    END IF;

    UPDATE internal_rpc_authority.authority_readback_attestation_challenges
    SET consumed_at = accepted_at
    WHERE challenge_id = challenge.challenge_id;
    INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts (
        receipt_id,
        challenge_id,
        semantic_request_digest_sha256,
        evidence_digest_sha256,
        verifier_generation,
        accepted_at,
        expires_at,
        evidence_jti,
        idempotency_key,
        peer_spiffe_id
    )
    VALUES (
        p_receipt_id,
        p_challenge_id,
        p_semantic_request_digest_sha256,
        p_evidence_digest_sha256,
        p_verifier_generation,
        accepted_at,
        accepted_at + make_interval(secs => (SELECT maximum_age_seconds
            FROM internal_rpc_authority.authority_freshness_policy WHERE singleton)),
        p_evidence_jti,
        p_idempotency_key,
        challenge.peer_spiffe_id
    );
    INSERT INTO internal_rpc_authority.authority_snapshot_readbacks (
        readback_id,
        workload_id,
        role,
        workload_generation,
        source_revision,
        digest_sha256,
        verified_at
    )
    VALUES (
        p_receipt_id,
        intent.workload_id,
        intent.role,
        intent.workload_generation,
        intent.source_revision,
        intent.served_state_digest_sha256,
        accepted_at
    )
    ON CONFLICT (
        workload_id,
        role,
        workload_generation,
        source_revision
    ) DO UPDATE
    SET digest_sha256 =
        internal_rpc_authority.authority_snapshot_readbacks.digest_sha256
    WHERE internal_rpc_authority.authority_snapshot_readbacks.digest_sha256 =
        EXCLUDED.digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'readback snapshot same-revision mutation rejected';
    END IF;
    RETURN p_receipt_id;
END
$_$;
-- +goose StatementEnd

-- Старый consumer после CAS также ограничивается исходным accepted_at+30s.
-- Старые receipts/history не переписываются и не получают нового срока.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION "internal_rpc_authority"."validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text") RETURNS boolean
    LANGUAGE "sql" VOLATILE SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $$
    SELECT EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_readback_attestation_receipts
            AS receipt
        JOIN internal_rpc_authority.authority_readback_attestation_challenges
            AS challenge
          ON challenge.challenge_id = receipt.challenge_id
         AND challenge.consumed_at IS NOT NULL
        JOIN internal_rpc_authority.authority_readback_intents AS intent
          ON intent.intent_id = challenge.intent_id
        JOIN internal_rpc_authority.authority_snapshot_history AS history
          ON history.source_revision = intent.source_revision
         AND history.source_digest_sha256 =
             intent.served_state_digest_sha256
        JOIN internal_rpc_authority.authority_rotation_intents AS rotation
          ON rotation.intent_id = history.publication_intent_id
         AND rotation.source_revision = history.source_revision
         AND rotation.source_digest_sha256 =
             history.source_digest_sha256
         AND rotation.status = 'PROMOTED'
        WHERE receipt.receipt_id = p_receipt_id
          AND receipt.expires_at > pg_catalog.clock_timestamp()
          AND receipt.accepted_at <= pg_catalog.clock_timestamp()
          AND receipt.accepted_at + make_interval(secs => (
              SELECT maximum_age_seconds
              FROM internal_rpc_authority.authority_freshness_policy WHERE singleton
          )) > pg_catalog.clock_timestamp()
          AND receipt.peer_spiffe_id = intent.workload_spiffe_id
          AND intent.kind = 'SNAPSHOT'
          AND intent.status = 'PINNED'
          AND intent.expires_at > pg_catalog.clock_timestamp()
          AND intent.workload_id = p_workload_id
          AND intent.source_revision = p_source_revision
          AND intent.served_state_digest_sha256 = p_source_digest_sha256
    );
$$;
-- +goose StatementEnd

-- Helper раскрывает только проверенный срок того же receipt и exact binding.
-- Внешняя query дополнительно проходит workload RLS текущего watermark.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.snapshot_attestation_freshness_deadline(
    p_receipt_id uuid, p_workload_id text, p_source_revision bigint, p_source_digest_sha256 text
) RETURNS timestamptz
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
    SELECT least(receipt.expires_at, receipt.accepted_at + interval '30 seconds', intent.expires_at)
    FROM internal_rpc_authority.authority_readback_attestation_receipts AS receipt
    JOIN internal_rpc_authority.authority_readback_attestation_challenges AS challenge
      ON challenge.challenge_id = receipt.challenge_id
    JOIN internal_rpc_authority.authority_readback_intents AS intent
      ON intent.intent_id = challenge.intent_id
    WHERE receipt.receipt_id = p_receipt_id
      AND internal_rpc_authority.validate_snapshot_attestation_receipt(
          p_receipt_id, p_workload_id, p_source_revision, p_source_digest_sha256)
      AND receipt.accepted_at + interval '30 seconds' > clock_timestamp();
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.snapshot_attestation_freshness_deadline(uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.snapshot_attestation_freshness_deadline(uuid,text,bigint,text)
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;

-- Issuer не получает table write: время и исходный receipt назначает owner.
CREATE TABLE internal_rpc_authority.authority_issued_context_bindings (
    jti uuid PRIMARY KEY,
    canonical_digest_sha256 text NOT NULL CHECK (canonical_digest_sha256 ~ '^[a-f0-9]{64}$'),
    caller_workload_id text NOT NULL,
    target_workload_id text NOT NULL,
    source_revision bigint NOT NULL,
    source_digest_sha256 text NOT NULL,
    key_set_revision bigint NOT NULL,
    policy_revision bigint NOT NULL,
    signer_generation bigint NOT NULL,
    receipt_id uuid NOT NULL REFERENCES internal_rpc_authority.authority_readback_attestation_receipts(receipt_id),
    valid_until timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    registered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    parent_jti uuid,
    CHECK (valid_until <= expires_at)
);
ALTER TABLE internal_rpc_authority.authority_issued_context_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_issued_context_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY issued_context_binding_owner ON internal_rpc_authority.authority_issued_context_bindings
    TO internal_rpc_authority_readback_owner USING (true) WITH CHECK (true);
REVOKE ALL ON internal_rpc_authority.authority_issued_context_bindings FROM PUBLIC;
CREATE POLICY snapshot_binding_owner_read ON internal_rpc_authority.authority_snapshot_watermarks
    FOR SELECT TO internal_rpc_authority_readback_owner
    USING (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'ISSUER')
        OR pg_has_role(session_user, 'internal_rpc_authority_migrator', 'MEMBER'));
CREATE POLICY accepted_parent_binding_owner_read ON internal_rpc_authority.authority_replay_reservations
    FOR SELECT TO internal_rpc_authority_readback_owner
    USING (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'ISSUER'));

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.register_issued_context_binding(
    p_jti uuid, p_digest text, p_caller text, p_target text,
    p_source bigint, p_source_digest text, p_keys bigint, p_policy bigint, p_signer bigint,
    p_issued_at timestamptz, p_expires_at timestamptz, p_parent uuid, p_parent_digest text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE
    old_binding internal_rpc_authority.authority_issued_context_bindings%ROWTYPE;
    served internal_rpc_authority.authority_snapshot_watermarks%ROWTYPE;
    deadline timestamptz;
    parent_deadline timestamptz;
BEGIN
    IF NOT internal_rpc_authority.workload_database_identity_allows_work(p_caller, 'ISSUER')
       OR p_jti IS NULL OR p_digest !~ '^[a-f0-9]{64}$' OR p_source_digest !~ '^[a-f0-9]{64}$'
       OR p_target IS NULL OR p_target = '' OR p_issued_at > clock_timestamp()
       OR p_expires_at <= clock_timestamp() OR p_expires_at > p_issued_at + interval '30 seconds'
       OR (p_parent IS NULL AND p_expires_at <> p_issued_at + interval '30 seconds') THEN
        RETURN false;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('issued_context:' || p_jti::text, 0));
    SELECT * INTO old_binding FROM internal_rpc_authority.authority_issued_context_bindings WHERE jti = p_jti;
    IF FOUND THEN
        RETURN (old_binding.canonical_digest_sha256, old_binding.caller_workload_id,
                old_binding.target_workload_id, old_binding.source_revision, old_binding.source_digest_sha256,
                old_binding.key_set_revision, old_binding.policy_revision, old_binding.signer_generation,
                old_binding.expires_at, old_binding.parent_jti)
            IS NOT DISTINCT FROM (p_digest, p_caller, p_target, p_source, p_source_digest, p_keys, p_policy, p_signer, p_expires_at, p_parent)
            AND old_binding.valid_until > clock_timestamp()
            AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
                old_binding.receipt_id, p_caller, p_source, p_source_digest) IS NOT NULL;
    END IF;
    SELECT * INTO served FROM internal_rpc_authority.authority_snapshot_watermarks
        WHERE target_workload_id = p_caller AND source_revision = p_source AND source_digest_sha256 = p_source_digest
          AND key_set_revision = p_keys AND policy_revision = p_policy AND signer_generation = p_signer;
    IF NOT FOUND THEN RETURN false; END IF;
    deadline := internal_rpc_authority.snapshot_attestation_freshness_deadline(served.readback_attestation_receipt_id, p_caller, p_source, p_source_digest);
    IF deadline IS NULL THEN RETURN false; END IF;
    IF p_parent IS NOT NULL THEN
        SELECT LEAST(parent.valid_until, parent.expires_at) INTO parent_deadline
          FROM internal_rpc_authority.authority_issued_context_bindings AS parent
          JOIN internal_rpc_authority.authority_replay_reservations AS accepted
            ON accepted.target_workload_id = parent.target_workload_id AND accepted.jti = parent.jti
               AND accepted.canonical_digest_sha256 = parent.canonical_digest_sha256
         WHERE parent.jti = p_parent AND parent.target_workload_id = p_caller
           AND parent.canonical_digest_sha256 = p_parent_digest
           AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
               parent.receipt_id, parent.caller_workload_id, parent.source_revision, parent.source_digest_sha256) IS NOT NULL;
        IF parent_deadline IS NULL OR parent_deadline <= clock_timestamp() THEN RETURN false; END IF;
        deadline := LEAST(deadline, parent_deadline);
    END IF;
    INSERT INTO internal_rpc_authority.authority_issued_context_bindings
        (jti,canonical_digest_sha256,caller_workload_id,target_workload_id,source_revision,source_digest_sha256,
         key_set_revision,policy_revision,signer_generation,receipt_id,valid_until,expires_at,parent_jti)
    VALUES (p_jti,p_digest,p_caller,p_target,p_source,p_source_digest,p_keys,p_policy,p_signer,
            served.readback_attestation_receipt_id,LEAST(deadline,p_expires_at),p_expires_at,p_parent);
    RETURN true;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.register_issued_context_binding(uuid,text,text,text,bigint,text,bigint,bigint,bigint,timestamptz,timestamptz,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.register_issued_context_binding(uuid,text,text,text,bigint,text,bigint,bigint,bigint,timestamptz,timestamptz,uuid,text)
    TO internal_rpc_authority_issuer;

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.validate_issued_context_binding(
    p_jti uuid, p_digest text, p_caller text, p_target text,
    p_source bigint, p_source_digest text, p_keys bigint, p_policy bigint, p_signer bigint
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE b internal_rpc_authority.authority_issued_context_bindings%ROWTYPE;
BEGIN
    IF NOT internal_rpc_authority.workload_database_identity_allows_work(p_target, 'VERIFIER') THEN RETURN false; END IF;
    SELECT * INTO b FROM internal_rpc_authority.authority_issued_context_bindings WHERE jti = p_jti;
    IF NOT FOUND THEN
        RETURN (SELECT version = 1 FROM internal_rpc_authority.authority_freshness_policy WHERE singleton);
    END IF;
    RETURN (b.canonical_digest_sha256,b.caller_workload_id,b.target_workload_id,b.source_revision,b.source_digest_sha256,
            b.key_set_revision,b.policy_revision,b.signer_generation)
       IS NOT DISTINCT FROM (p_digest,p_caller,p_target,p_source,p_source_digest,p_keys,p_policy,p_signer)
       AND b.valid_until > clock_timestamp() AND b.expires_at > clock_timestamp()
       AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
           b.receipt_id,b.caller_workload_id,b.source_revision,b.source_digest_sha256) IS NOT NULL;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.validate_issued_context_binding(uuid,text,text,text,bigint,text,bigint,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.validate_issued_context_binding(uuid,text,text,text,bigint,text,bigint,bigint,bigint)
    TO internal_rpc_authority_verifier;

-- Read-only resolution после UNKNOWN регистрации. Повторный INSERT и новый JTI
-- не выполняются; возвращается только точная ещё действующая запись.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.read_issued_context_binding(
    p_jti uuid, p_digest text, p_caller text, p_target text,
    p_source bigint, p_source_digest text, p_keys bigint, p_policy bigint, p_signer bigint,
    p_issued_at timestamptz, p_expires_at timestamptz, p_parent uuid, p_parent_digest text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE b internal_rpc_authority.authority_issued_context_bindings%ROWTYPE;
BEGIN
    IF NOT internal_rpc_authority.workload_database_identity_allows_work(p_caller, 'ISSUER') THEN RETURN false; END IF;
    SELECT * INTO b FROM internal_rpc_authority.authority_issued_context_bindings WHERE jti = p_jti;
    IF NOT FOUND THEN RETURN false; END IF;
    RETURN (b.canonical_digest_sha256,b.caller_workload_id,b.target_workload_id,b.source_revision,b.source_digest_sha256,
            b.key_set_revision,b.policy_revision,b.signer_generation,b.expires_at,b.parent_jti)
       IS NOT DISTINCT FROM (p_digest,p_caller,p_target,p_source,p_source_digest,p_keys,p_policy,p_signer,p_expires_at,p_parent)
       AND p_issued_at <= clock_timestamp() AND p_expires_at <= p_issued_at + interval '30 seconds'
       AND (p_parent IS NOT NULL OR p_expires_at = p_issued_at + interval '30 seconds')
       AND (p_parent IS NULL OR EXISTS (
           SELECT 1 FROM internal_rpc_authority.authority_issued_context_bindings AS parent
           WHERE parent.jti = p_parent AND parent.canonical_digest_sha256 = p_parent_digest
             AND parent.target_workload_id = p_caller AND parent.valid_until > clock_timestamp()))
       AND b.valid_until > clock_timestamp() AND b.expires_at > clock_timestamp()
       AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
           b.receipt_id,b.caller_workload_id,b.source_revision,b.source_digest_sha256) IS NOT NULL;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.read_issued_context_binding(uuid,text,text,text,bigint,text,bigint,bigint,bigint,timestamptz,timestamptz,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.read_issued_context_binding(uuid,text,text,text,bigint,text,bigint,bigint,bigint,timestamptz,timestamptz,uuid,text)
    TO internal_rpc_authority_issuer;

-- Только истёкшие более 10 минут назад bindings своего caller; runtime не может
-- удалить активную запись и затем зарегистрировать старый JTI с новым receipt.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.cleanup_issued_context_bindings(p_caller text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE removed bigint;
BEGIN
    IF NOT internal_rpc_authority.workload_database_identity_allows_work(p_caller, 'ISSUER') THEN RETURN 0; END IF;
    DELETE FROM internal_rpc_authority.authority_issued_context_bindings
     WHERE caller_workload_id = p_caller AND expires_at < clock_timestamp() - interval '10 minutes'
       AND registered_at < clock_timestamp() - interval '10 minutes';
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.cleanup_issued_context_bindings(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.cleanup_issued_context_bindings(text) TO internal_rpc_authority_issuer;

-- Без payload/key/credential: migrator наблюдает возраст точного receipt.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.authority_freshness_consumer_status() RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
BEGIN
    IF NOT pg_has_role(session_user, 'internal_rpc_authority_migrator', 'MEMBER') THEN
        RAISE EXCEPTION 'freshness readback identity rejected' USING ERRCODE = '42501';
    END IF;
    RETURN (SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'workload', w.target_workload_id, 'sourceRevision', w.source_revision,
        'sourceSHA256', w.source_digest_sha256, 'keySetRevision', w.key_set_revision,
        'policyRevision', w.policy_revision, 'signerGeneration', w.signer_generation,
        'receiptSHA256', encode(sha256(convert_to(w.readback_attestation_receipt_id::text, 'UTF8')), 'hex'),
        'acceptedAt', r.accepted_at, 'freshnessDeadline', internal_rpc_authority.snapshot_attestation_freshness_deadline(
            w.readback_attestation_receipt_id,w.target_workload_id,w.source_revision,w.source_digest_sha256)
    ) ORDER BY w.target_workload_id), '[]'::jsonb)
    FROM internal_rpc_authority.authority_snapshot_watermarks AS w
    JOIN internal_rpc_authority.authority_readback_attestation_receipts AS r
      ON r.receipt_id = w.readback_attestation_receipt_id);
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.authority_freshness_consumer_status() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.authority_freshness_consumer_status() TO internal_rpc_authority_migrator;

RESET ROLE;

-- +goose Down
-- Forward-only: deadline activation и receipt/replay history не откатываются.
SELECT 1;
