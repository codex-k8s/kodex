-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;
RESET ROLE;
-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_publisher_g5') THEN
        CREATE ROLE ira_publisher_g5
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles WHERE rolname = 'ira_readback_attestor_g5'
    ) THEN
        CREATE ROLE ira_readback_attestor_g5
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
END
$roles$;
-- +goose StatementEnd

SET ROLE internal_rpc_authority_owner;
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
               AND requested_generation BETWEEN 1 AND 5
           )
           OR (
               requested_capability = 'READBACK_ATTESTOR'
               AND requested_principal =
                   'ira_readback_attestor_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 5
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
        request_id, canonical_request_digest_sha256
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
        capability, principal, generation, lifecycle_status,
        registered_set_digest_sha256, reconciled_at, retired_at
    )
    VALUES (
        requested_capability, requested_principal, requested_generation,
        requested_status, requested_registered_set_digest_sha256,
        clock_timestamp(), NULL
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
               AND requested_generation BETWEEN 1 AND 5
           )
           OR (
               requested_capability = 'READBACK_ATTESTOR'
               AND requested_principal =
                   'ira_readback_attestor_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 5
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
        request_id, canonical_request_digest_sha256
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
        capability, principal, generation, lifecycle_status,
        registered_set_digest_sha256, reconciled_at, retired_at
    )
    VALUES (
        requested_capability, requested_principal, requested_generation,
        'RETIRED', requested_registered_set_digest_sha256,
        clock_timestamp(), clock_timestamp()
    )
    ON CONFLICT (capability, generation) DO UPDATE
    SET lifecycle_status = 'RETIRED',
        registered_set_digest_sha256 = EXCLUDED.registered_set_digest_sha256,
        reconciled_at = EXCLUDED.reconciled_at,
        retired_at = EXCLUDED.retired_at
    WHERE internal_rpc_authority.authority_runtime_database_identities.principal =
            EXCLUDED.principal
      AND internal_rpc_authority.authority_runtime_database_identities
            .lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED')
    RETURNING true INTO accepted;
    RETURN coalesce(accepted, false);
END
$function$;

CREATE TABLE internal_rpc_authority.database_credential_rotation_intents (
    request_id uuid PRIMARY KEY,
    canonical_digest_sha256 text NOT NULL
        CHECK (canonical_digest_sha256 ~ '^[a-f0-9]{64}$'),
    phase text NOT NULL CHECK (phase IN (
        'CREATED',
        'PRE_ROTATE_CHECKPOINTED',
        'NEXT_STAGED',
        'NEXT_READ_BACK',
        'PROMOTED',
        'CURRENT_ROLLED_OUT',
        'COMPLETED'
    )),
    pre_rotation_digests jsonb NOT NULL DEFAULT '{}'::jsonb,
    staged_digests jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (jsonb_typeof(pre_rotation_digests) = 'object'),
    CHECK (jsonb_typeof(staged_digests) = 'object')
);

CREATE TABLE internal_rpc_authority.database_credential_session_readbacks (
    capability text NOT NULL
        CHECK (capability IN ('PUBLISHER', 'READBACK_ATTESTOR')),
    generation bigint NOT NULL
        CHECK (generation BETWEEN 1 AND 9007199254740991),
    lifecycle_status text NOT NULL
        CHECK (lifecycle_status IN ('CURRENT', 'NEXT')),
    principal text NOT NULL,
    credential_digest_sha256 text NOT NULL
        CHECK (credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
    pod_uid uuid NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (capability, generation, lifecycle_status, pod_uid)
);

ALTER TABLE internal_rpc_authority.database_credential_rotation_intents
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.database_credential_session_readbacks
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.database_credential_rotation_intents
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.database_credential_rotation_intents
    FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.database_credential_session_readbacks
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.database_credential_session_readbacks
    FORCE ROW LEVEL SECURITY;

CREATE POLICY database_credential_rotation_intents_owner
    ON internal_rpc_authority.database_credential_rotation_intents
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY database_credential_rotation_intents_reconciler
    ON internal_rpc_authority.database_credential_rotation_intents
    TO internal_rpc_authority_database_credential_reconciler
    USING (true)
    WITH CHECK (true);
CREATE POLICY database_credential_session_readbacks_owner
    ON internal_rpc_authority.database_credential_session_readbacks
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);
CREATE POLICY database_credential_session_readbacks_reconciler_read
    ON internal_rpc_authority.database_credential_session_readbacks
    FOR SELECT
    TO internal_rpc_authority_database_credential_reconciler
    USING (true);

GRANT SELECT, INSERT, UPDATE
    ON internal_rpc_authority.database_credential_rotation_intents
    TO internal_rpc_authority_database_credential_reconciler;
GRANT SELECT
    ON internal_rpc_authority.database_credential_session_readbacks
    TO internal_rpc_authority_database_credential_reconciler;

CREATE FUNCTION internal_rpc_authority.record_database_credential_session_readback(
    p_credential_digest_sha256 text,
    p_pod_uid uuid
)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
    identity internal_rpc_authority.authority_runtime_database_identities%ROWTYPE;
BEGIN
    IF p_credential_digest_sha256 !~ '^[a-f0-9]{64}$' THEN
        RAISE EXCEPTION 'database credential readback digest rejected';
    END IF;
    SELECT *
    INTO identity
    FROM internal_rpc_authority.authority_runtime_database_identities
    WHERE principal = session_user
      AND lifecycle_status IN ('CURRENT', 'NEXT');
    IF NOT FOUND
       OR NOT (
           (
               identity.capability = 'PUBLISHER'
               AND pg_catalog.pg_has_role(
                   session_user,
                   'internal_rpc_authority_publisher',
                   'MEMBER'
               )
           )
           OR (
               identity.capability = 'READBACK_ATTESTOR'
               AND pg_catalog.pg_has_role(
                   session_user,
                   'internal_rpc_authority_readback_attestor',
                   'MEMBER'
               )
           )
       )
    THEN
        RAISE EXCEPTION 'database credential session identity rejected';
    END IF;

    INSERT INTO internal_rpc_authority.database_credential_session_readbacks (
        capability,
        generation,
        lifecycle_status,
        principal,
        credential_digest_sha256,
        pod_uid,
        observed_at
    )
    VALUES (
        identity.capability,
        identity.generation,
        identity.lifecycle_status,
        session_user,
        p_credential_digest_sha256,
        p_pod_uid,
        clock_timestamp()
    )
    ON CONFLICT (capability, generation, lifecycle_status, pod_uid) DO UPDATE
    SET principal = EXCLUDED.principal,
        credential_digest_sha256 = EXCLUDED.credential_digest_sha256,
        observed_at = EXCLUDED.observed_at;
    RETURN identity.lifecycle_status;
END
$function$;

ALTER FUNCTION internal_rpc_authority.record_database_credential_session_readback(
    text, uuid
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION
internal_rpc_authority.record_database_credential_session_readback(
    text, uuid
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
internal_rpc_authority.record_database_credential_session_readback(
    text, uuid
) TO internal_rpc_authority_publisher,
     internal_rpc_authority_readback_attestor;

REVOKE CREATE ON SCHEMA internal_rpc_authority FROM PUBLIC;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
