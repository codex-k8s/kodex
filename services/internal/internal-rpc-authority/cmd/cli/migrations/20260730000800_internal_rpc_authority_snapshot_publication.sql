-- +goose Up
-- Полный snapshot graph принадлежит publisher, но таблицы и переходы
-- остаются у отдельного NOLOGIN owner. Runtime получает только exact EXECUTE.
ALTER TABLE internal_rpc_authority.authority_snapshot_history
    ADD COLUMN snapshot_compact_jws text,
    ADD COLUMN publication_intent_id uuid UNIQUE,
    ADD COLUMN publication_input_digest_sha256 text
        CHECK (publication_input_digest_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN expected_readback_count integer
        CHECK (expected_readback_count BETWEEN 1 AND 384);

CREATE UNIQUE INDEX authority_snapshot_readbacks_exact_target_revision
    ON internal_rpc_authority.authority_snapshot_readbacks (
        workload_id,
        role,
        workload_generation,
        source_revision
    );

ALTER TABLE internal_rpc_authority.authority_snapshot_history
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_snapshot_history
    FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_intents
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_intents
    FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
    FORCE ROW LEVEL SECURITY;

CREATE POLICY authority_snapshot_history_owner
    ON internal_rpc_authority.authority_snapshot_history
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY authority_snapshot_history_publisher_read
    ON internal_rpc_authority.authority_snapshot_history
    FOR SELECT
    TO internal_rpc_authority_publisher
    USING (true);
CREATE POLICY authority_rotation_intents_owner
    ON internal_rpc_authority.authority_rotation_intents
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY authority_rotation_intents_publisher_read
    ON internal_rpc_authority.authority_rotation_intents
    FOR SELECT
    TO internal_rpc_authority_publisher
    USING (true);
CREATE POLICY authority_snapshot_readbacks_owner
    ON internal_rpc_authority.authority_snapshot_readbacks
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY authority_snapshot_readbacks_publisher_read
    ON internal_rpc_authority.authority_snapshot_readbacks
    FOR SELECT
    TO internal_rpc_authority_publisher
    USING (true);

GRANT SELECT ON internal_rpc_authority.authority_snapshot_history
    TO internal_rpc_authority_publisher;
GRANT SELECT ON internal_rpc_authority.authority_rotation_intents
    TO internal_rpc_authority_publisher;
GRANT SELECT ON internal_rpc_authority.authority_snapshot_readbacks
    TO internal_rpc_authority_publisher;

CREATE FUNCTION internal_rpc_authority.publisher_append_snapshot_history(
    p_source_revision bigint,
    p_source_digest_sha256 text,
    p_key_set_revision bigint,
    p_policy_revision bigint,
    p_signer_generation bigint,
    p_predecessor_revision bigint,
    p_predecessor_digest_sha256 text,
    p_snapshot_compact_jws text,
    p_publication_intent_id uuid,
    p_publication_input_digest_sha256 text,
    p_expected_readback_count integer
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    latest internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    existing internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    zero_digest constant text :=
        '0000000000000000000000000000000000000000000000000000000000000000';
BEGIN
    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            'internal_rpc_authority.publisher_snapshot_history',
            0
        )
    );
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_key_set_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_policy_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_signer_generation NOT BETWEEN 1 AND 9007199254740991
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_publication_input_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_predecessor_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR pg_catalog.octet_length(p_snapshot_compact_jws) NOT BETWEEN 64 AND 8192
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
    THEN
        RETURN false;
    END IF;

    SELECT *
    INTO existing
    FROM internal_rpc_authority.authority_snapshot_history
    WHERE source_revision = p_source_revision
    FOR UPDATE;
    IF FOUND THEN
        RETURN existing.source_digest_sha256 = p_source_digest_sha256
           AND existing.key_set_revision = p_key_set_revision
           AND existing.policy_revision = p_policy_revision
           AND existing.signer_generation = p_signer_generation
           AND existing.predecessor_revision = p_predecessor_revision
           AND existing.predecessor_digest_sha256 =
                p_predecessor_digest_sha256
           AND existing.snapshot_compact_jws = p_snapshot_compact_jws
           AND existing.publication_intent_id = p_publication_intent_id
           AND existing.publication_input_digest_sha256 =
                p_publication_input_digest_sha256
           AND existing.expected_readback_count = p_expected_readback_count;
    END IF;

    SELECT *
    INTO latest
    FROM internal_rpc_authority.authority_snapshot_history
    ORDER BY source_revision DESC
    LIMIT 1
    FOR UPDATE;
    IF p_source_revision = 1 THEN
        IF FOUND
           OR p_predecessor_revision <> 0
           OR p_predecessor_digest_sha256 <> zero_digest
        THEN
            RETURN false;
        END IF;
    ELSIF NOT FOUND
       OR p_source_revision <> latest.source_revision + 1
       OR p_predecessor_revision <> latest.source_revision
       OR p_predecessor_digest_sha256 <> latest.source_digest_sha256
    THEN
        RETURN false;
    END IF;

    INSERT INTO internal_rpc_authority.authority_snapshot_history (
        source_revision,
        source_digest_sha256,
        key_set_revision,
        policy_revision,
        signer_generation,
        predecessor_revision,
        predecessor_digest_sha256,
        canonical_payload,
        published_at,
        snapshot_compact_jws,
        publication_intent_id,
        publication_input_digest_sha256,
        expected_readback_count
    )
    VALUES (
        p_source_revision,
        p_source_digest_sha256,
        p_key_set_revision,
        p_policy_revision,
        p_signer_generation,
        p_predecessor_revision,
        p_predecessor_digest_sha256,
        pg_catalog.jsonb_build_object(
            'source_revision', p_source_revision,
            'source_digest_sha256', p_source_digest_sha256
        ),
        pg_catalog.clock_timestamp(),
        p_snapshot_compact_jws,
        p_publication_intent_id,
        p_publication_input_digest_sha256,
        p_expected_readback_count
    );
    INSERT INTO internal_rpc_authority.authority_rotation_intents (
        intent_id,
        source_revision,
        source_digest_sha256,
        status,
        created_at,
        updated_at
    )
    VALUES (
        p_publication_intent_id,
        p_source_revision,
        p_source_digest_sha256,
        'PREPARED',
        pg_catalog.clock_timestamp(),
        pg_catalog.clock_timestamp()
    );
    RETURN true;
END
$function$;

CREATE FUNCTION internal_rpc_authority.publisher_promote_snapshot(
    p_publication_intent_id uuid,
    p_source_revision bigint,
    p_source_digest_sha256 text,
    p_expected_readback_count integer
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    matched integer;
    publication internal_rpc_authority.authority_snapshot_history%ROWTYPE;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
    THEN
        RETURN false;
    END IF;
    SELECT *
    INTO publication
    FROM internal_rpc_authority.authority_snapshot_history
    WHERE publication_intent_id = p_publication_intent_id
      AND source_revision = p_source_revision
      AND source_digest_sha256 = p_source_digest_sha256
      AND expected_readback_count = p_expected_readback_count
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    SELECT pg_catalog.count(*)::integer
    INTO matched
    FROM internal_rpc_authority.authority_snapshot_readbacks
    WHERE source_revision = p_source_revision
      AND digest_sha256 = p_source_digest_sha256;
    IF matched <> p_expected_readback_count THEN
        RETURN false;
    END IF;
    UPDATE internal_rpc_authority.authority_rotation_intents
    SET status = 'PROMOTED',
        updated_at = pg_catalog.clock_timestamp()
    WHERE intent_id = p_publication_intent_id
      AND source_revision = p_source_revision
      AND source_digest_sha256 = p_source_digest_sha256
      AND status IN ('PREPARED', 'DELIVERED', 'PROMOTED');
    RETURN FOUND;
END
$function$;

CREATE OR REPLACE FUNCTION
internal_rpc_authority.validate_snapshot_attestation_receipt(
    p_receipt_id uuid,
    p_workload_id text,
    p_source_revision bigint,
    p_source_digest_sha256 text
)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
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
          AND receipt.peer_spiffe_id = intent.workload_spiffe_id
          AND intent.kind = 'SNAPSHOT'
          AND intent.status = 'PINNED'
          AND intent.expires_at > pg_catalog.clock_timestamp()
          AND intent.workload_id = p_workload_id
          AND intent.source_revision = p_source_revision
          AND intent.served_state_digest_sha256 = p_source_digest_sha256
    );
$function$;

CREATE OR REPLACE FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    p_challenge_id uuid,
    p_receipt_id uuid,
    p_evidence_jti uuid,
    p_evidence_digest_sha256 text,
    p_verifier_generation bigint,
    p_idempotency_key uuid,
    p_semantic_request_digest_sha256 text
)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    existing internal_rpc_authority.authority_readback_attestation_receipts%ROWTYPE;
    challenge internal_rpc_authority.authority_readback_attestation_challenges%ROWTYPE;
    intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
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
    accepted_at := pg_catalog.clock_timestamp();

    SELECT *
    INTO existing
    FROM internal_rpc_authority.authority_readback_attestation_receipts
    WHERE idempotency_key = p_idempotency_key;
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
        accepted_at + interval '5 minutes',
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
$function$;

ALTER FUNCTION internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text, integer
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.publisher_promote_snapshot(
    uuid, bigint, text, integer
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.validate_snapshot_attestation_receipt(
    uuid, text, bigint, text
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION
internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text, integer
) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_promote_snapshot(
    uuid, bigint, text, integer
) FROM PUBLIC;
REVOKE ALL ON FUNCTION
internal_rpc_authority.validate_snapshot_attestation_receipt(
    uuid, text, bigint, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text, integer
) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_promote_snapshot(
    uuid, bigint, text, integer
) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.validate_snapshot_attestation_receipt(
    uuid, text, bigint, text
) TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
