-- +goose Up
ALTER TABLE internal_rpc_authority.authority_readback_intents
    ADD COLUMN workload_spiffe_id text NOT NULL
        CHECK (workload_spiffe_id ~ '^spiffe://mattercodex[.]local/'),
    ADD COLUMN material_generation bigint NOT NULL
        CHECK (material_generation BETWEEN 1 AND 9007199254740991),
    ADD COLUMN possession_key_kid text NOT NULL
        CHECK (possession_key_kid ~ '^[A-Za-z0-9._-]{3,64}$'),
    ADD COLUMN possession_key_generation_exact bigint NOT NULL
        CHECK (possession_key_generation_exact BETWEEN 1 AND 9007199254740991),
    ADD COLUMN possession_public_jwk jsonb NOT NULL,
    ADD COLUMN possession_key_thumbprint_sha256 text NOT NULL
        CHECK (possession_key_thumbprint_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN source_revision bigint NOT NULL
        CHECK (source_revision BETWEEN 1 AND 9007199254740991),
    ADD COLUMN served_state_digest_sha256 text NOT NULL
        CHECK (served_state_digest_sha256 ~ '^[a-f0-9]{64}$');

ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
    ADD COLUMN peer_spiffe_id text NOT NULL
        CHECK (peer_spiffe_id ~ '^spiffe://mattercodex[.]local/'),
    ADD COLUMN readback_credential_jti uuid NOT NULL,
    ADD COLUMN readback_credential_digest_sha256 text NOT NULL
        CHECK (readback_credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN idempotency_key uuid NOT NULL,
    ADD COLUMN semantic_request_digest_sha256 text NOT NULL
        CHECK (semantic_request_digest_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN challenge_digest_sha256 text NOT NULL
        CHECK (challenge_digest_sha256 ~ '^[a-f0-9]{64}$');

CREATE UNIQUE INDEX authority_readback_challenge_idempotency_idx
    ON internal_rpc_authority.authority_readback_attestation_challenges (
        peer_spiffe_id,
        idempotency_key
    );

ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
    ADD COLUMN evidence_jti uuid NOT NULL,
    ADD COLUMN idempotency_key uuid NOT NULL,
    ADD COLUMN peer_spiffe_id text NOT NULL
        CHECK (peer_spiffe_id ~ '^spiffe://mattercodex[.]local/');

CREATE UNIQUE INDEX authority_readback_receipt_idempotency_idx
    ON internal_rpc_authority.authority_readback_attestation_receipts (
        peer_spiffe_id,
        idempotency_key
    );
CREATE UNIQUE INDEX authority_readback_receipt_evidence_jti_idx
    ON internal_rpc_authority.authority_readback_attestation_receipts (
        evidence_jti
    );

ALTER TABLE internal_rpc_authority.authority_readback_intents
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_intents
    FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
    FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
    FORCE ROW LEVEL SECURITY;

CREATE POLICY authority_readback_intents_attestor
    ON internal_rpc_authority.authority_readback_intents
    FOR SELECT
    TO internal_rpc_authority_readback_attestor
    USING (true);
CREATE POLICY authority_readback_challenges_attestor
    ON internal_rpc_authority.authority_readback_attestation_challenges
    TO internal_rpc_authority_readback_attestor
    USING (true)
    WITH CHECK (true);
CREATE POLICY authority_readback_receipts_attestor
    ON internal_rpc_authority.authority_readback_attestation_receipts
    TO internal_rpc_authority_readback_attestor
    USING (true)
    WITH CHECK (true);

GRANT SELECT ON internal_rpc_authority.authority_readback_intents
    TO internal_rpc_authority_readback_attestor;
GRANT SELECT, INSERT, UPDATE
    ON internal_rpc_authority.authority_readback_attestation_challenges
    TO internal_rpc_authority_readback_attestor;
GRANT SELECT, INSERT
    ON internal_rpc_authority.authority_readback_attestation_receipts
    TO internal_rpc_authority_readback_attestor;

ALTER TABLE internal_rpc_authority.authority_restore_fences
    ADD CONSTRAINT authority_restore_fences_phase_check
    CHECK (phase IN (
        'OPEN',
        'QUIESCING',
        'PREPARED',
        'RESTORING',
        'COMPLETED',
        'FENCED_SAFE_WINDOW'
    )) NOT VALID;

ALTER TABLE internal_rpc_authority.authority_restore_fences
    VALIDATE CONSTRAINT authority_restore_fences_phase_check;

CREATE FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work()
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
    SELECT NOT EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_restore_fences AS fence
        WHERE fence.phase IN (
            'QUIESCING',
            'PREPARED',
            'RESTORING',
            'FENCED_SAFE_WINDOW'
        )
           OR (
               fence.phase = 'COMPLETED'
               AND (
                   fence.safe_window_not_before IS NULL
                   OR fence.safe_window_not_before > clock_timestamp()
               )
           )
    );
$function$;

ALTER FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work()
    OWNER TO internal_rpc_authority_readback_owner;

REVOKE ALL ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;

-- +goose Down
DROP INDEX internal_rpc_authority.authority_readback_receipt_evidence_jti_idx;
DROP INDEX internal_rpc_authority.authority_readback_receipt_idempotency_idx;
DROP INDEX internal_rpc_authority.authority_readback_challenge_idempotency_idx;
REVOKE ALL ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    FROM internal_rpc_authority_issuer, internal_rpc_authority_verifier;
DROP FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work();
ALTER TABLE internal_rpc_authority.authority_restore_fences
    DROP CONSTRAINT authority_restore_fences_phase_check;
