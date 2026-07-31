-- +goose Up
DO $roles$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'internal_rpc_authority_restore_controller'
    ) THEN
        CREATE ROLE internal_rpc_authority_restore_controller
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_restore_controller_g1'
    ) THEN
        CREATE ROLE ira_restore_controller_g1
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
END
$roles$;

ALTER ROLE internal_rpc_authority_restore_controller
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
ALTER ROLE ira_restore_controller_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
GRANT internal_rpc_authority_restore_controller TO ira_restore_controller_g1;
GRANT USAGE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_restore_controller;

INSERT INTO internal_rpc_authority.authority_restore_fences (
    database_cluster_id,
    restore_epoch,
    phase,
    evidence_digest_sha256,
    safe_window_not_before,
    updated_at
)
VALUES (
    'internal-rpc-authority-primary',
    1,
    'OPEN',
    '0000000000000000000000000000000000000000000000000000000000000000',
    NULL,
    clock_timestamp()
)
ON CONFLICT (database_cluster_id) DO NOTHING;

CREATE OR REPLACE FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
    SELECT count(*) = 1
       AND bool_and(
           fence.phase = 'OPEN'
           OR (
               fence.phase = 'COMPLETED'
               AND fence.safe_window_not_before IS NOT NULL
               AND fence.safe_window_not_before <= clock_timestamp()
           )
       )
    FROM internal_rpc_authority.authority_restore_fences AS fence;
$function$;

ALTER FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work()
    OWNER TO internal_rpc_authority_readback_owner;

CREATE FUNCTION internal_rpc_authority.apply_restore_fence(
    p_database_cluster_id text,
    p_restore_epoch bigint,
    p_phase text,
    p_evidence_digest_sha256 text,
    p_safe_window_not_before timestamptz
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    applied boolean;
BEGIN
    IF p_database_cluster_id <> 'internal-rpc-authority-primary'
       OR p_restore_epoch < 1
       OR p_phase NOT IN (
           'OPEN', 'QUIESCING', 'PREPARED', 'RESTORING', 'COMPLETED'
       )
       OR p_evidence_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR (p_phase = 'COMPLETED' AND p_safe_window_not_before IS NULL)
       OR (p_phase <> 'COMPLETED' AND p_safe_window_not_before IS NOT NULL)
    THEN
        RETURN false;
    END IF;

    INSERT INTO internal_rpc_authority.authority_restore_fences (
        database_cluster_id,
        restore_epoch,
        phase,
        evidence_digest_sha256,
        safe_window_not_before,
        updated_at
    )
    VALUES (
        p_database_cluster_id,
        p_restore_epoch,
        p_phase,
        p_evidence_digest_sha256,
        p_safe_window_not_before,
        clock_timestamp()
    )
    ON CONFLICT (database_cluster_id) DO UPDATE
    SET restore_epoch = EXCLUDED.restore_epoch,
        phase = EXCLUDED.phase,
        evidence_digest_sha256 = EXCLUDED.evidence_digest_sha256,
        safe_window_not_before = EXCLUDED.safe_window_not_before,
        updated_at = EXCLUDED.updated_at
    WHERE internal_rpc_authority.authority_restore_fences.restore_epoch
              <= EXCLUDED.restore_epoch
      AND (
          internal_rpc_authority.authority_restore_fences.restore_epoch
              < EXCLUDED.restore_epoch
          OR internal_rpc_authority.authority_restore_fences.evidence_digest_sha256
              = EXCLUDED.evidence_digest_sha256
      )
      AND CASE internal_rpc_authority.authority_restore_fences.phase
          WHEN 'OPEN' THEN EXCLUDED.phase IN ('OPEN', 'QUIESCING')
          WHEN 'QUIESCING' THEN EXCLUDED.phase IN ('QUIESCING', 'PREPARED')
          WHEN 'PREPARED' THEN EXCLUDED.phase IN ('PREPARED', 'RESTORING', 'COMPLETED')
          WHEN 'RESTORING' THEN EXCLUDED.phase IN ('RESTORING', 'COMPLETED')
          WHEN 'COMPLETED' THEN EXCLUDED.phase = 'COMPLETED'
          ELSE false
      END
    RETURNING true INTO applied;

    RETURN coalesce(applied, false);
END
$function$;

ALTER FUNCTION internal_rpc_authority.apply_restore_fence(
    text, bigint, text, text, timestamptz
)
    OWNER TO internal_rpc_authority_readback_owner;

REVOKE ALL ON FUNCTION
    internal_rpc_authority.apply_restore_fence(
        text, bigint, text, text, timestamptz
    )
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.apply_restore_fence(
        text, bigint, text, text, timestamptz
    )
    TO internal_rpc_authority_restore_controller;

CREATE TABLE internal_rpc_authority.authority_publisher_delivery_receipts (
    idempotency_key uuid PRIMARY KEY,
    directive_jti uuid NOT NULL UNIQUE,
    directive_digest_sha256 text NOT NULL
        CHECK (directive_digest_sha256 ~ '^[a-f0-9]{64}$'),
    delivery_receipt_compact_jws text NOT NULL
        CHECK (octet_length(delivery_receipt_compact_jws) BETWEEN 64 AND 8192),
    role_credential_digest_sha256 text NOT NULL
        CHECK (role_credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
    credential_generation bigint NOT NULL
        CHECK (credential_generation BETWEEN 1 AND 9007199254740991),
    ack_key_generation bigint NOT NULL
        CHECK (ack_key_generation BETWEEN 1 AND 9007199254740991),
    accepted_at timestamptz NOT NULL
);
ALTER TABLE internal_rpc_authority.authority_publisher_delivery_receipts
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_publisher_delivery_receipts
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_publisher_delivery_receipts
    FORCE ROW LEVEL SECURITY;
CREATE POLICY authority_publisher_delivery_receipts_runtime
    ON internal_rpc_authority.authority_publisher_delivery_receipts
    TO internal_rpc_authority_publisher
    USING (true)
    WITH CHECK (true);
GRANT SELECT, INSERT
    ON internal_rpc_authority.authority_publisher_delivery_receipts
    TO internal_rpc_authority_publisher;

CREATE POLICY authority_readback_intents_publisher
    ON internal_rpc_authority.authority_readback_intents
    FOR SELECT
    TO internal_rpc_authority_publisher
    USING (true);
GRANT SELECT ON internal_rpc_authority.authority_readback_intents
    TO internal_rpc_authority_publisher;

REVOKE ALL ON ALL TABLES IN SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE CREATE ON SCHEMA internal_rpc_authority FROM PUBLIC;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
