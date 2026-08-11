-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;
-- Attestor не владеет строками challenge/receipt и не может обходить
-- server-derived binding прямым DML. Единственная изменяющая граница —
-- точные SECURITY DEFINER функции владельца состояния.
REVOKE INSERT, UPDATE, DELETE
    ON internal_rpc_authority.authority_readback_attestation_challenges
    FROM internal_rpc_authority_readback_attestor;
REVOKE INSERT, UPDATE, DELETE
    ON internal_rpc_authority.authority_readback_attestation_receipts
    FROM internal_rpc_authority_readback_attestor;

DROP POLICY IF EXISTS authority_readback_challenges_attestor
    ON internal_rpc_authority.authority_readback_attestation_challenges;
DROP POLICY IF EXISTS authority_readback_receipts_attestor
    ON internal_rpc_authority.authority_readback_attestation_receipts;
CREATE POLICY authority_readback_challenges_attestor_read
    ON internal_rpc_authority.authority_readback_attestation_challenges
    FOR SELECT
    TO internal_rpc_authority_readback_attestor
    USING (internal_rpc_authority.runtime_restore_fence_allows_work());
CREATE POLICY authority_readback_receipts_attestor_read
    ON internal_rpc_authority.authority_readback_attestation_receipts
    FOR SELECT
    TO internal_rpc_authority_readback_attestor
    USING (true);
CREATE POLICY authority_readback_challenges_owner_write
    ON internal_rpc_authority.authority_readback_attestation_challenges
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY authority_readback_receipts_owner_write
    ON internal_rpc_authority.authority_readback_attestation_receipts
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION
internal_rpc_authority.issue_authority_readback_attestation_challenge(
    p_intent_id uuid,
    p_challenge_id uuid,
    p_challenge_jti uuid,
    p_challenge_nonce text,
    p_challenge_digest_sha256 text,
    p_readback_credential_jti uuid,
    p_readback_credential_digest_sha256 text,
    p_idempotency_key uuid,
    p_semantic_request_digest_sha256 text
)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    accepted_id uuid;
    issued_at timestamptz;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_readback_attestor',
        'MEMBER'
    )
       OR p_readback_credential_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_semantic_request_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_challenge_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR pg_catalog.octet_length(p_challenge_nonce) NOT BETWEEN 32 AND 256
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RAISE EXCEPTION 'readback challenge binding rejected';
    END IF;
    issued_at := pg_catalog.clock_timestamp();

    INSERT INTO internal_rpc_authority.authority_readback_attestation_challenges (
        challenge_id,
        challenge_jti,
        intent_id,
        request_digest_sha256,
        nonce,
        issued_at,
        expires_at,
        peer_spiffe_id,
        readback_credential_jti,
        readback_credential_digest_sha256,
        idempotency_key,
        semantic_request_digest_sha256,
        challenge_digest_sha256
    )
    SELECT
        p_challenge_id,
        p_challenge_jti,
        intent.intent_id,
        p_semantic_request_digest_sha256,
        p_challenge_nonce,
        issued_at,
        issued_at + interval '30 seconds',
        intent.workload_spiffe_id,
        p_readback_credential_jti,
        p_readback_credential_digest_sha256,
        p_idempotency_key,
        p_semantic_request_digest_sha256,
        p_challenge_digest_sha256
    FROM internal_rpc_authority.authority_readback_intents AS intent
    WHERE intent.intent_id = p_intent_id
      AND intent.status = 'PINNED'
      AND intent.expires_at >= issued_at + interval '30 seconds'
    ON CONFLICT (peer_spiffe_id, idempotency_key) DO UPDATE
    SET semantic_request_digest_sha256 =
        internal_rpc_authority.authority_readback_attestation_challenges
            .semantic_request_digest_sha256
    WHERE internal_rpc_authority.authority_readback_attestation_challenges
            .semantic_request_digest_sha256 =
            EXCLUDED.semantic_request_digest_sha256
      AND internal_rpc_authority.authority_readback_attestation_challenges
            .intent_id = EXCLUDED.intent_id
      AND internal_rpc_authority.authority_readback_attestation_challenges
            .readback_credential_digest_sha256 =
            EXCLUDED.readback_credential_digest_sha256
    RETURNING challenge_id INTO accepted_id;

    RETURN accepted_id;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
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
    RETURN p_receipt_id;
END
$function$;
-- +goose StatementEnd

ALTER FUNCTION
internal_rpc_authority.issue_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, text, uuid, text, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION
internal_rpc_authority.issue_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, text, uuid, text, uuid, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.issue_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, text, uuid, text, uuid, text
) TO internal_rpc_authority_readback_attestor;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) TO internal_rpc_authority_readback_attestor;

CREATE TABLE internal_rpc_authority.authority_readback_trust_watermarks (
    attestor_id text PRIMARY KEY,
    root_id text NOT NULL,
    root_fingerprint_sha256 text NOT NULL
        CHECK (root_fingerprint_sha256 ~ '^[a-f0-9]{64}$'),
    manifest_bundle_revision bigint NOT NULL
        CHECK (manifest_bundle_revision BETWEEN 1 AND 9007199254740991),
    manifest_bundle_digest_sha256 text NOT NULL
        CHECK (manifest_bundle_digest_sha256 ~ '^[a-f0-9]{64}$'),
    trust_source_revision bigint NOT NULL
        CHECK (trust_source_revision BETWEEN 1 AND 9007199254740991),
    trust_set_digest_sha256 text NOT NULL
        CHECK (trust_set_digest_sha256 ~ '^[a-f0-9]{64}$'),
    trust_key_set_revision bigint NOT NULL
        CHECK (trust_key_set_revision BETWEEN 1 AND 9007199254740991),
    signer_generation bigint NOT NULL
        CHECK (signer_generation BETWEEN 1 AND 9007199254740991),
    predecessor_state_digest_sha256 text NOT NULL
        CHECK (predecessor_state_digest_sha256 ~ '^[a-f0-9]{64}$'),
    served_state_digest_sha256 text NOT NULL
        CHECK (served_state_digest_sha256 ~ '^[a-f0-9]{64}$'),
    served_at timestamptz NOT NULL
);
ALTER TABLE internal_rpc_authority.authority_readback_trust_watermarks
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_trust_watermarks
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_trust_watermarks
    FORCE ROW LEVEL SECURITY;
CREATE POLICY authority_readback_trust_owner
    ON internal_rpc_authority.authority_readback_trust_watermarks
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY authority_readback_trust_attestor_read
    ON internal_rpc_authority.authority_readback_trust_watermarks
    FOR SELECT
    TO internal_rpc_authority_readback_attestor
    USING (true);
GRANT SELECT
    ON internal_rpc_authority.authority_readback_trust_watermarks
    TO internal_rpc_authority_readback_attestor;

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.activate_readback_trust(
    p_root_id text,
    p_root_fingerprint_sha256 text,
    p_manifest_bundle_revision bigint,
    p_manifest_bundle_digest_sha256 text,
    p_trust_source_revision bigint,
    p_trust_set_digest_sha256 text,
    p_trust_key_set_revision bigint,
    p_signer_generation bigint,
    p_predecessor_state_digest_sha256 text,
    p_served_state_digest_sha256 text,
    p_served_at timestamptz
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    current internal_rpc_authority.authority_readback_trust_watermarks%ROWTYPE;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_readback_attestor',
        'MEMBER'
    )
       OR p_root_id <> 'internal-rpc-authority-readback-manifest-root-v1'
       OR p_root_fingerprint_sha256 !~ '^[a-f0-9]{64}$'
       OR p_manifest_bundle_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_trust_set_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_predecessor_state_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_served_state_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_manifest_bundle_revision < 1
       OR p_trust_source_revision < 1
       OR p_trust_key_set_revision < 1
       OR p_signer_generation < 1
    THEN
        RETURN false;
    END IF;

    SELECT *
    INTO current
    FROM internal_rpc_authority.authority_readback_trust_watermarks
    WHERE attestor_id = 'internal-rpc-authority-readback-attestor'
    FOR UPDATE;
    IF FOUND THEN
        IF p_manifest_bundle_revision < current.manifest_bundle_revision
           OR p_trust_source_revision < current.trust_source_revision
           OR p_trust_key_set_revision < current.trust_key_set_revision
           OR p_signer_generation < current.signer_generation
        THEN
            RETURN false;
        END IF;
        IF p_manifest_bundle_revision = current.manifest_bundle_revision
           AND p_trust_source_revision = current.trust_source_revision
           AND p_trust_key_set_revision = current.trust_key_set_revision
        THEN
            RETURN p_served_state_digest_sha256 =
                current.served_state_digest_sha256
               AND p_manifest_bundle_digest_sha256 =
                current.manifest_bundle_digest_sha256
               AND p_trust_set_digest_sha256 =
                current.trust_set_digest_sha256;
        END IF;
        IF p_predecessor_state_digest_sha256 <>
            current.served_state_digest_sha256
        THEN
            RETURN false;
        END IF;
    END IF;

    INSERT INTO internal_rpc_authority.authority_readback_trust_watermarks (
        attestor_id,
        root_id,
        root_fingerprint_sha256,
        manifest_bundle_revision,
        manifest_bundle_digest_sha256,
        trust_source_revision,
        trust_set_digest_sha256,
        trust_key_set_revision,
        signer_generation,
        predecessor_state_digest_sha256,
        served_state_digest_sha256,
        served_at
    )
    VALUES (
        'internal-rpc-authority-readback-attestor',
        p_root_id,
        p_root_fingerprint_sha256,
        p_manifest_bundle_revision,
        p_manifest_bundle_digest_sha256,
        p_trust_source_revision,
        p_trust_set_digest_sha256,
        p_trust_key_set_revision,
        p_signer_generation,
        p_predecessor_state_digest_sha256,
        p_served_state_digest_sha256,
        p_served_at
    )
    ON CONFLICT (attestor_id) DO UPDATE
    SET root_id = EXCLUDED.root_id,
        root_fingerprint_sha256 = EXCLUDED.root_fingerprint_sha256,
        manifest_bundle_revision = EXCLUDED.manifest_bundle_revision,
        manifest_bundle_digest_sha256 =
            EXCLUDED.manifest_bundle_digest_sha256,
        trust_source_revision = EXCLUDED.trust_source_revision,
        trust_set_digest_sha256 = EXCLUDED.trust_set_digest_sha256,
        trust_key_set_revision = EXCLUDED.trust_key_set_revision,
        signer_generation = EXCLUDED.signer_generation,
        predecessor_state_digest_sha256 =
            EXCLUDED.predecessor_state_digest_sha256,
        served_state_digest_sha256 = EXCLUDED.served_state_digest_sha256,
        served_at = EXCLUDED.served_at;
    RETURN true;
END
$function$;
-- +goose StatementEnd

ALTER FUNCTION internal_rpc_authority.activate_readback_trust(
    text, text, bigint, text, bigint, text, bigint, bigint, text, text,
    timestamptz
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION internal_rpc_authority.activate_readback_trust(
    text, text, bigint, text, bigint, text, bigint, bigint, text, text,
    timestamptz
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.activate_readback_trust(
    text, text, bigint, text, bigint, text, bigint, bigint, text, text,
    timestamptz
) TO internal_rpc_authority_readback_attestor;

REVOKE CREATE ON SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA internal_rpc_authority FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.issue_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, text, uuid, text, uuid, text
) TO internal_rpc_authority_readback_attestor;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) TO internal_rpc_authority_readback_attestor;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.activate_readback_trust(
    text, text, bigint, text, bigint, text, bigint, bigint, text, text,
    timestamptz
) TO internal_rpc_authority_readback_attestor;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
