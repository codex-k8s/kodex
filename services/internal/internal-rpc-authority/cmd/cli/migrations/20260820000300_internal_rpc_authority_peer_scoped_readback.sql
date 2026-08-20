-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;

-- Receipt idempotency принадлежит transport peer. Сначала разрешаем peer из
-- серверного challenge, затем используем существующий составной unique index.
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
-- +goose StatementEnd

ALTER FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
) TO internal_rpc_authority_readback_attestor;

RESET ROLE;

-- +goose Down
-- Forward-only: rollback is performed by a separate compensating migration.
