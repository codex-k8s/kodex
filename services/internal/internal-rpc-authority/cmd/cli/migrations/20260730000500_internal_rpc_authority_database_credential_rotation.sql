-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;
RESET ROLE;
-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'internal_rpc_authority_credential_lifecycle_definer'
    ) THEN
        CREATE ROLE internal_rpc_authority_credential_lifecycle_definer
            NOLOGIN NOSUPERUSER NOCREATEDB CREATEROLE INHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_publisher_g3') THEN
        CREATE ROLE ira_publisher_g3
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_publisher_g4') THEN
        CREATE ROLE ira_publisher_g4
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_readback_attestor_g3'
    ) THEN
        CREATE ROLE ira_readback_attestor_g3
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_readback_attestor_g4'
    ) THEN
        CREATE ROLE ira_readback_attestor_g4
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
END
$roles$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $role_safety$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_roles
        WHERE rolname IN (
            'internal_rpc_authority_credential_lifecycle_definer',
            'ira_publisher_g3',
            'ira_publisher_g4',
            'ira_readback_attestor_g3',
            'ira_readback_attestor_g4'
        )
          AND (rolsuper OR rolcreatedb OR rolreplication OR rolbypassrls)
    ) THEN
        RAISE EXCEPTION 'credential lifecycle role has prohibited attributes'
            USING ERRCODE = '42501';
    END IF;
END
$role_safety$;
-- +goose StatementEnd

ALTER ROLE internal_rpc_authority_credential_lifecycle_definer
    NOLOGIN CREATEROLE INHERIT;
GRANT pg_signal_backend
    TO internal_rpc_authority_credential_lifecycle_definer;
GRANT internal_rpc_authority_credential_lifecycle_definer
    TO internal_rpc_authority_owner
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;

SET ROLE internal_rpc_authority_owner;
GRANT USAGE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_credential_lifecycle_definer;
GRANT CREATE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_credential_lifecycle_definer;
GRANT SELECT, INSERT, UPDATE
    ON internal_rpc_authority.authority_runtime_database_identities,
       internal_rpc_authority.database_credential_reconciliation_receipts
    TO internal_rpc_authority_credential_lifecycle_definer;

CREATE POLICY authority_runtime_database_identities_lifecycle_definer
    ON internal_rpc_authority.authority_runtime_database_identities
    TO internal_rpc_authority_credential_lifecycle_definer
    USING (true)
    WITH CHECK (true);
CREATE POLICY database_credential_receipts_lifecycle_definer
    ON internal_rpc_authority.database_credential_reconciliation_receipts
    TO internal_rpc_authority_credential_lifecycle_definer
    USING (true)
    WITH CHECK (true);

CREATE OR REPLACE FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
    requested_capability text,
    requested_principal text,
    requested_generation bigint,
    requested_status text,
    requested_request_id uuid,
    requested_registered_set_digest_sha256 text
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    capability_role text;
    accepted boolean;
BEGIN
    IF NOT pg_has_role(
        session_user,
        'internal_rpc_authority_database_credential_reconciler',
        'MEMBER'
    ) THEN
        RAISE EXCEPTION 'database credential reconciler identity rejected';
    END IF;
    IF requested_status NOT IN ('CURRENT', 'NEXT', 'PREVIOUS')
       OR requested_registered_set_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR NOT (
           (
               requested_capability = 'PUBLISHER'
               AND requested_principal =
                   'ira_publisher_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 4
           )
           OR (
               requested_capability = 'READBACK_ATTESTOR'
               AND requested_principal =
                   'ira_readback_attestor_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 4
           )
       )
    THEN
        RAISE EXCEPTION 'database credential registry tuple rejected';
    END IF;

    capability_role := CASE requested_capability
        WHEN 'PUBLISHER' THEN 'internal_rpc_authority_publisher'
        WHEN 'READBACK_ATTESTOR' THEN 'internal_rpc_authority_readback_attestor'
    END;

    EXECUTE format('GRANT %I TO %I', capability_role, requested_principal);
    IF requested_status IN ('CURRENT', 'NEXT') THEN
        EXECUTE format('ALTER ROLE %I LOGIN', requested_principal);
    ELSE
        EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal);
    END IF;

    INSERT INTO internal_rpc_authority.database_credential_reconciliation_receipts (
        request_id,
        canonical_request_digest_sha256
    )
    VALUES (requested_request_id, requested_registered_set_digest_sha256)
    ON CONFLICT (request_id) DO UPDATE
    SET canonical_request_digest_sha256 =
        internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256
    WHERE internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256 =
        EXCLUDED.canonical_request_digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'database credential idempotency conflict';
    END IF;

    INSERT INTO internal_rpc_authority.authority_runtime_database_identities (
        capability,
        principal,
        generation,
        lifecycle_status,
        registered_set_digest_sha256,
        reconciled_at,
        retired_at
    )
    VALUES (
        requested_capability,
        requested_principal,
        requested_generation,
        requested_status,
        requested_registered_set_digest_sha256,
        clock_timestamp(),
        NULL
    )
    ON CONFLICT (capability, generation) DO UPDATE
    SET lifecycle_status = EXCLUDED.lifecycle_status,
        registered_set_digest_sha256 = EXCLUDED.registered_set_digest_sha256,
        reconciled_at = EXCLUDED.reconciled_at,
        retired_at = NULL
    WHERE internal_rpc_authority.authority_runtime_database_identities.principal =
            EXCLUDED.principal
      AND CASE
          internal_rpc_authority.authority_runtime_database_identities
              .lifecycle_status
          WHEN 'NEXT' THEN EXCLUDED.lifecycle_status IN (
              'NEXT', 'CURRENT', 'PREVIOUS'
          )
          WHEN 'CURRENT' THEN EXCLUDED.lifecycle_status IN (
              'CURRENT', 'PREVIOUS'
          )
          WHEN 'PREVIOUS' THEN EXCLUDED.lifecycle_status = 'PREVIOUS'
          ELSE false
      END
    RETURNING true INTO accepted;
    RETURN coalesce(accepted, false);
END
$function$;

CREATE OR REPLACE FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    requested_capability text,
    requested_principal text,
    requested_generation bigint,
    requested_request_id uuid,
    requested_registered_set_digest_sha256 text
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    capability_role text;
    accepted boolean;
BEGIN
    IF NOT pg_has_role(
        session_user,
        'internal_rpc_authority_database_credential_reconciler',
        'MEMBER'
    ) THEN
        RAISE EXCEPTION 'database credential reconciler identity rejected';
    END IF;
    IF requested_registered_set_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR NOT (
           (
               requested_capability = 'PUBLISHER'
               AND requested_principal =
                   'ira_publisher_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 4
           )
           OR (
               requested_capability = 'READBACK_ATTESTOR'
               AND requested_principal =
                   'ira_readback_attestor_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 4
           )
       )
    THEN
        RAISE EXCEPTION 'database credential retirement tuple rejected';
    END IF;

    capability_role := CASE requested_capability
        WHEN 'PUBLISHER' THEN 'internal_rpc_authority_publisher'
        WHEN 'READBACK_ATTESTOR' THEN 'internal_rpc_authority_readback_attestor'
    END;

    EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal);
    EXECUTE format('REVOKE %I FROM %I', capability_role, requested_principal);
    PERFORM pg_terminate_backend(activity.pid)
    FROM pg_stat_activity AS activity
    WHERE activity.usename = requested_principal
      AND activity.pid <> pg_backend_pid();

    INSERT INTO internal_rpc_authority.database_credential_reconciliation_receipts (
        request_id,
        canonical_request_digest_sha256
    )
    VALUES (requested_request_id, requested_registered_set_digest_sha256)
    ON CONFLICT (request_id) DO UPDATE
    SET canonical_request_digest_sha256 =
        internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256
    WHERE internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256 =
        EXCLUDED.canonical_request_digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'database credential idempotency conflict';
    END IF;

    INSERT INTO internal_rpc_authority.authority_runtime_database_identities (
        capability,
        principal,
        generation,
        lifecycle_status,
        registered_set_digest_sha256,
        reconciled_at,
        retired_at
    )
    VALUES (
        requested_capability,
        requested_principal,
        requested_generation,
        'RETIRED',
        requested_registered_set_digest_sha256,
        clock_timestamp(),
        clock_timestamp()
    )
    ON CONFLICT (capability, generation) DO UPDATE
    SET lifecycle_status = 'RETIRED',
        registered_set_digest_sha256 = EXCLUDED.registered_set_digest_sha256,
        reconciled_at = EXCLUDED.reconciled_at,
        retired_at = EXCLUDED.retired_at
    WHERE internal_rpc_authority.authority_runtime_database_identities.principal =
            EXCLUDED.principal
      AND internal_rpc_authority.authority_runtime_database_identities
            .lifecycle_status IN (
                'CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED'
            )
    RETURNING true INTO accepted;
    RETURN coalesce(accepted, false);
END
$function$;

REVOKE ALL ON FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
    text, text, bigint, text, uuid, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    text, text, bigint, uuid, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
    text, text, bigint, text, uuid, text
) TO internal_rpc_authority_database_credential_reconciler;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    text, text, bigint, uuid, text
) TO internal_rpc_authority_database_credential_reconciler;

ALTER FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
    text, text, bigint, text, uuid, text
) OWNER TO internal_rpc_authority_credential_lifecycle_definer;
ALTER FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    text, text, bigint, uuid, text
) OWNER TO internal_rpc_authority_credential_lifecycle_definer;
REVOKE CREATE ON SCHEMA internal_rpc_authority
    FROM internal_rpc_authority_credential_lifecycle_definer;
RESET ROLE;
REVOKE internal_rpc_authority_credential_lifecycle_definer
    FROM internal_rpc_authority_owner;
SET ROLE internal_rpc_authority_owner;

REVOKE CREATE ON SCHEMA internal_rpc_authority FROM PUBLIC;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
