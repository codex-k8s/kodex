-- +goose Up
DROP POLICY IF EXISTS authority_readback_intents_publisher
    ON internal_rpc_authority.authority_readback_intents;

CREATE POLICY authority_readback_intents_publisher
    ON internal_rpc_authority.authority_readback_intents
    TO internal_rpc_authority_publisher
    USING (true)
    WITH CHECK (
        status = 'PINNED'
        AND kind IN ('KEY_DELIVERY', 'SNAPSHOT')
        AND expires_at > pg_catalog.clock_timestamp()
        AND internal_rpc_authority.runtime_restore_fence_allows_work()
    );

GRANT SELECT, INSERT
    ON internal_rpc_authority.authority_readback_intents
    TO internal_rpc_authority_publisher;

CREATE POLICY authority_readback_intents_owner_read
    ON internal_rpc_authority.authority_readback_intents
    FOR SELECT
    TO internal_rpc_authority_readback_owner
    USING (true);
CREATE POLICY authority_readback_challenges_owner_read
    ON internal_rpc_authority.authority_readback_attestation_challenges
    FOR SELECT
    TO internal_rpc_authority_readback_owner
    USING (true);
CREATE POLICY authority_readback_receipts_owner_read
    ON internal_rpc_authority.authority_readback_attestation_receipts
    FOR SELECT
    TO internal_rpc_authority_readback_owner
    USING (true);

DROP POLICY IF EXISTS authority_readback_challenges_attestor
    ON internal_rpc_authority.authority_readback_attestation_challenges;
CREATE POLICY authority_readback_challenges_attestor
    ON internal_rpc_authority.authority_readback_attestation_challenges
    TO internal_rpc_authority_readback_attestor
    USING (internal_rpc_authority.runtime_restore_fence_allows_work())
    WITH CHECK (internal_rpc_authority.runtime_restore_fence_allows_work());

DROP POLICY IF EXISTS authority_readback_receipts_attestor
    ON internal_rpc_authority.authority_readback_attestation_receipts;
CREATE POLICY authority_readback_receipts_attestor
    ON internal_rpc_authority.authority_readback_attestation_receipts
    TO internal_rpc_authority_readback_attestor
    USING (internal_rpc_authority.runtime_restore_fence_allows_work())
    WITH CHECK (internal_rpc_authority.runtime_restore_fence_allows_work());

ALTER TABLE internal_rpc_authority.authority_snapshot_watermarks
    ADD COLUMN readback_attestation_receipt_id uuid
        REFERENCES internal_rpc_authority.authority_readback_attestation_receipts(receipt_id);

CREATE FUNCTION internal_rpc_authority.validate_snapshot_attestation_receipt(
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
        FROM internal_rpc_authority.authority_readback_attestation_receipts AS receipt
        JOIN internal_rpc_authority.authority_readback_attestation_challenges AS challenge
          ON challenge.challenge_id = receipt.challenge_id
         AND challenge.consumed_at IS NOT NULL
        JOIN internal_rpc_authority.authority_readback_intents AS intent
          ON intent.intent_id = challenge.intent_id
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

ALTER FUNCTION internal_rpc_authority.validate_snapshot_attestation_receipt(
    uuid, text, bigint, text
)
    OWNER TO internal_rpc_authority_readback_owner;

REVOKE ALL ON FUNCTION
    internal_rpc_authority.validate_snapshot_attestation_receipt(
        uuid, text, bigint, text
    )
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.validate_snapshot_attestation_receipt(
        uuid, text, bigint, text
    )
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;

GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    TO internal_rpc_authority_publisher,
       internal_rpc_authority_readback_attestor;

REVOKE ALL ON ALL TABLES IN SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE CREATE ON SCHEMA internal_rpc_authority FROM PUBLIC;

GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.validate_snapshot_attestation_receipt(
        uuid, text, bigint, text
    )
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;
GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    TO internal_rpc_authority_publisher,
       internal_rpc_authority_readback_attestor;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
