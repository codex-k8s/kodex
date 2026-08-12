-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;

-- Publication time belongs to the signed snapshot. Persist it before external
-- delivery so every publisher replica can reproduce the winning publication.
-- +goose StatementBegin
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
    p_expected_readback_count integer,
    p_published_at timestamp with time zone
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
       OR pg_catalog.octet_length(p_snapshot_compact_jws)
            NOT BETWEEN 64 AND 1048576
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
       OR p_published_at IS NULL
       OR p_published_at < pg_catalog.clock_timestamp() - interval '5 minutes'
       OR p_published_at > pg_catalog.clock_timestamp() + interval '5 seconds'
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
           AND existing.expected_readback_count = p_expected_readback_count
           AND existing.published_at = p_published_at;
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
        p_published_at,
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
-- +goose StatementEnd

ALTER FUNCTION internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text,
    integer, timestamp with time zone
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION
internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text,
    integer, timestamp with time zone
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION
internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text, integer
) FROM internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.publisher_append_snapshot_history(
    bigint, text, bigint, bigint, bigint, bigint, text, text, uuid, text,
    integer, timestamp with time zone
) TO internal_rpc_authority_publisher;

-- +goose Down
-- Forward-only: rollback is a separate compensating migration.
