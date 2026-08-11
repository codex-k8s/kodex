-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;
CREATE SCHEMA IF NOT EXISTS internal_rpc_authority;

RESET ROLE;
-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'internal_rpc_authority_readback_owner') THEN
        CREATE ROLE internal_rpc_authority_readback_owner
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'internal_rpc_authority_issuer') THEN
        CREATE ROLE internal_rpc_authority_issuer
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'internal_rpc_authority_verifier') THEN
        CREATE ROLE internal_rpc_authority_verifier
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'internal_rpc_authority_publisher') THEN
        CREATE ROLE internal_rpc_authority_publisher
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'internal_rpc_authority_readback_attestor') THEN
        CREATE ROLE internal_rpc_authority_readback_attestor
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'internal_rpc_authority_database_credential_reconciler'
    ) THEN
        CREATE ROLE internal_rpc_authority_database_credential_reconciler
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'internal_rpc_authority_recovery') THEN
        CREATE ROLE internal_rpc_authority_recovery
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_publisher_g1') THEN
        CREATE ROLE ira_publisher_g1
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_publisher_g2') THEN
        CREATE ROLE ira_publisher_g2
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_readback_attestor_g1') THEN
        CREATE ROLE ira_readback_attestor_g1
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ira_readback_attestor_g2') THEN
        CREATE ROLE ira_readback_attestor_g2
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_control_api_gateway_issuer_g1'
    ) THEN
        CREATE ROLE ira_control_api_gateway_issuer_g1
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_control_api_gateway_issuer_g2'
    ) THEN
        CREATE ROLE ira_control_api_gateway_issuer_g2
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_control_plane_verifier_g1'
    ) THEN
        CREATE ROLE ira_control_plane_verifier_g1
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_control_plane_verifier_g2'
    ) THEN
        CREATE ROLE ira_control_plane_verifier_g2
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_database_credential_reconciler'
    ) THEN
        CREATE ROLE ira_database_credential_reconciler
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
END
$roles$;
-- +goose StatementEnd

-- PostgreSQL разрешает bounded CREATEROLE менять только непривилегированные
-- атрибуты. Привилегированные атрибуты проверяются fail-closed до ALTER ROLE.
-- +goose StatementBegin
DO $role_safety$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_roles
        WHERE rolname IN (
            'internal_rpc_authority_readback_owner',
            'internal_rpc_authority_issuer',
            'internal_rpc_authority_verifier',
            'internal_rpc_authority_publisher',
            'internal_rpc_authority_readback_attestor',
            'internal_rpc_authority_database_credential_reconciler',
            'internal_rpc_authority_recovery',
            'ira_publisher_g1',
            'ira_publisher_g2',
            'ira_readback_attestor_g1',
            'ira_readback_attestor_g2',
            'ira_control_api_gateway_issuer_g1',
            'ira_control_api_gateway_issuer_g2',
            'ira_control_plane_verifier_g1',
            'ira_control_plane_verifier_g2',
            'ira_database_credential_reconciler'
        )
          AND (rolsuper OR rolcreatedb OR rolreplication OR rolbypassrls)
    ) THEN
        RAISE EXCEPTION 'internal-rpc-authority managed role has prohibited attributes'
            USING ERRCODE = '42501';
    END IF;
END
$role_safety$;
-- +goose StatementEnd

ALTER ROLE internal_rpc_authority_readback_owner
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE internal_rpc_authority_issuer
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE internal_rpc_authority_verifier
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE internal_rpc_authority_publisher
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE internal_rpc_authority_readback_attestor
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE internal_rpc_authority_database_credential_reconciler
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE internal_rpc_authority_recovery
    NOLOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_publisher_g1
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_publisher_g2
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_readback_attestor_g1
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_readback_attestor_g2
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_control_api_gateway_issuer_g1
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_control_api_gateway_issuer_g2
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_control_plane_verifier_g1
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_control_plane_verifier_g2
    LOGIN NOCREATEROLE NOINHERIT;
ALTER ROLE ira_database_credential_reconciler
    LOGIN NOCREATEROLE NOINHERIT;

GRANT internal_rpc_authority_publisher TO ira_publisher_g1, ira_publisher_g2;
GRANT internal_rpc_authority_readback_attestor
    TO ira_readback_attestor_g1, ira_readback_attestor_g2;
GRANT internal_rpc_authority_issuer
    TO ira_control_api_gateway_issuer_g1, ira_control_api_gateway_issuer_g2;
GRANT internal_rpc_authority_verifier
    TO ira_control_plane_verifier_g1, ira_control_plane_verifier_g2;
GRANT internal_rpc_authority_database_credential_reconciler
    TO ira_database_credential_reconciler;
GRANT internal_rpc_authority_readback_owner TO internal_rpc_authority_owner
    WITH INHERIT TRUE, SET TRUE, ADMIN FALSE;

SET ROLE internal_rpc_authority_owner;
REVOKE ALL ON SCHEMA internal_rpc_authority FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_readback_owner;
GRANT USAGE ON SCHEMA internal_rpc_authority TO
    internal_rpc_authority_issuer,
    internal_rpc_authority_verifier,
    internal_rpc_authority_publisher,
    internal_rpc_authority_readback_attestor,
    internal_rpc_authority_database_credential_reconciler,
    internal_rpc_authority_recovery;

CREATE TABLE internal_rpc_authority.authority_snapshot_watermarks (
    target_workload_id text PRIMARY KEY
        CHECK (target_workload_id ~ '^[a-z0-9](?:[a-z0-9.-]{1,94}[a-z0-9])$'),
    source_revision bigint NOT NULL CHECK (source_revision BETWEEN 1 AND 9007199254740991),
    source_digest_sha256 text NOT NULL CHECK (source_digest_sha256 ~ '^[a-f0-9]{64}$'),
    key_set_revision bigint NOT NULL CHECK (key_set_revision BETWEEN 1 AND 9007199254740991),
    policy_revision bigint NOT NULL CHECK (policy_revision BETWEEN 1 AND 9007199254740991),
    signer_generation bigint NOT NULL CHECK (signer_generation BETWEEN 1 AND 9007199254740991),
    served_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_replay_reservations (
    target_workload_id text NOT NULL,
    jti uuid NOT NULL,
    canonical_digest_sha256 text NOT NULL CHECK (canonical_digest_sha256 ~ '^[a-f0-9]{64}$'),
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (target_workload_id, jti)
);

CREATE INDEX authority_replay_reservations_expiry_idx
    ON internal_rpc_authority.authority_replay_reservations (expires_at, accepted_at);

CREATE TABLE internal_rpc_authority.authority_proof_watermarks (
    caller_workload_id text NOT NULL,
    operation_id text NOT NULL,
    authority_proof_issuer text NOT NULL,
    proof_revision bigint NOT NULL CHECK (proof_revision BETWEEN 1 AND 9007199254740991),
    canonical_payload_digest_sha256 text NOT NULL
        CHECK (canonical_payload_digest_sha256 ~ '^[a-f0-9]{64}$'),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (caller_workload_id, operation_id, authority_proof_issuer)
);

CREATE TABLE internal_rpc_authority.authority_proof_reservations (
    caller_workload_id text NOT NULL,
    jti uuid NOT NULL,
    canonical_digest_sha256 text NOT NULL CHECK (canonical_digest_sha256 ~ '^[a-f0-9]{64}$'),
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (caller_workload_id, jti)
);

CREATE INDEX authority_proof_reservations_expiry_idx
    ON internal_rpc_authority.authority_proof_reservations (expires_at, accepted_at);

CREATE TABLE internal_rpc_authority.authority_runtime_database_identities (
    capability text NOT NULL
        CHECK (capability IN ('PUBLISHER', 'READBACK_ATTESTOR')),
    principal text NOT NULL UNIQUE,
    generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
    lifecycle_status text NOT NULL
        CHECK (lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED')),
    registered_set_digest_sha256 text NOT NULL
        CHECK (registered_set_digest_sha256 ~ '^[a-f0-9]{64}$'),
    reconciled_at timestamptz NOT NULL,
    retired_at timestamptz,
    PRIMARY KEY (capability, generation)
);

CREATE UNIQUE INDEX authority_runtime_database_identities_current_idx
    ON internal_rpc_authority.authority_runtime_database_identities (capability)
    WHERE lifecycle_status = 'CURRENT';

CREATE UNIQUE INDEX authority_runtime_database_identities_next_idx
    ON internal_rpc_authority.authority_runtime_database_identities (capability)
    WHERE lifecycle_status = 'NEXT';

CREATE TABLE internal_rpc_authority.database_credential_reconciliation_receipts (
    request_id uuid PRIMARY KEY,
    canonical_request_digest_sha256 text NOT NULL
        CHECK (canonical_request_digest_sha256 ~ '^[a-f0-9]{64}$'),
    reconciled_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE internal_rpc_authority.database_credential_reconciler_leases (
    lease_name text PRIMARY KEY
        CHECK (lease_name = 'database-credential-reconciler'),
    holder_id uuid NOT NULL,
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    lease_until timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

ALTER TABLE internal_rpc_authority.database_credential_reconciliation_receipts
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.database_credential_reconciliation_receipts
    FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.database_credential_reconciler_leases
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.database_credential_reconciler_leases
    FORCE ROW LEVEL SECURITY;

CREATE TABLE internal_rpc_authority.authority_snapshot_history (
    source_revision bigint PRIMARY KEY CHECK (source_revision BETWEEN 1 AND 9007199254740991),
    source_digest_sha256 text NOT NULL UNIQUE CHECK (source_digest_sha256 ~ '^[a-f0-9]{64}$'),
    key_set_revision bigint NOT NULL,
    policy_revision bigint NOT NULL,
    signer_generation bigint NOT NULL,
    predecessor_revision bigint NOT NULL,
    predecessor_digest_sha256 text NOT NULL CHECK (predecessor_digest_sha256 ~ '^[a-f0-9]{64}$'),
    canonical_payload jsonb NOT NULL,
    published_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_restore_fences (
    database_cluster_id text PRIMARY KEY,
    restore_epoch bigint NOT NULL,
    phase text NOT NULL,
    evidence_digest_sha256 text NOT NULL CHECK (evidence_digest_sha256 ~ '^[a-f0-9]{64}$'),
    safe_window_not_before timestamptz,
    updated_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_rotation_intents (
    intent_id uuid PRIMARY KEY,
    source_revision bigint NOT NULL,
    source_digest_sha256 text NOT NULL CHECK (source_digest_sha256 ~ '^[a-f0-9]{64}$'),
    status text NOT NULL CHECK (status IN ('PREPARED', 'DELIVERED', 'PROMOTED', 'ABORTED')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_key_delivery_readbacks (
    readback_id uuid PRIMARY KEY,
    workload_id text NOT NULL,
    role text NOT NULL,
    workload_generation bigint NOT NULL,
    source_revision bigint NOT NULL,
    digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
    verified_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_snapshot_readbacks (
    readback_id uuid PRIMARY KEY,
    workload_id text NOT NULL,
    role text NOT NULL,
    workload_generation bigint NOT NULL,
    source_revision bigint NOT NULL,
    digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
    verified_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_readback_intents (
    intent_id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('KEY_DELIVERY', 'SNAPSHOT')),
    intent_revision bigint NOT NULL,
    intent_digest_sha256 text NOT NULL CHECK (intent_digest_sha256 ~ '^[a-f0-9]{64}$'),
    workload_id text NOT NULL,
    role text NOT NULL,
    workload_generation bigint NOT NULL,
    credential_generation bigint NOT NULL,
    possession_key_generation bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('PINNED', 'ATTESTED', 'PROMOTED', 'EXPIRED')),
    expires_at timestamptz NOT NULL
);

CREATE TABLE internal_rpc_authority.authority_readback_attestation_challenges (
    challenge_id uuid PRIMARY KEY,
    challenge_jti uuid NOT NULL UNIQUE,
    intent_id uuid NOT NULL REFERENCES internal_rpc_authority.authority_readback_intents(intent_id),
    request_digest_sha256 text NOT NULL CHECK (request_digest_sha256 ~ '^[a-f0-9]{64}$'),
    nonce text NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz
);

CREATE TABLE internal_rpc_authority.authority_readback_attestation_receipts (
    receipt_id uuid PRIMARY KEY,
    challenge_id uuid NOT NULL UNIQUE
        REFERENCES internal_rpc_authority.authority_readback_attestation_challenges(challenge_id),
    semantic_request_digest_sha256 text NOT NULL
        CHECK (semantic_request_digest_sha256 ~ '^[a-f0-9]{64}$'),
    evidence_digest_sha256 text NOT NULL CHECK (evidence_digest_sha256 ~ '^[a-f0-9]{64}$'),
    verifier_generation bigint NOT NULL,
    accepted_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);

ALTER TABLE internal_rpc_authority.authority_snapshot_watermarks ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_snapshot_watermarks FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_replay_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_replay_reservations FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_proof_watermarks ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_proof_watermarks FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_proof_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_proof_reservations FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_runtime_database_identities ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_runtime_database_identities FORCE ROW LEVEL SECURITY;

CREATE POLICY authority_snapshot_watermarks_runtime
    ON internal_rpc_authority.authority_snapshot_watermarks
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier
    USING (true)
    WITH CHECK (true);

CREATE POLICY authority_replay_reservations_verifier
    ON internal_rpc_authority.authority_replay_reservations
    TO internal_rpc_authority_verifier
    USING (true)
    WITH CHECK (true);

CREATE POLICY authority_proof_watermarks_issuer
    ON internal_rpc_authority.authority_proof_watermarks
    TO internal_rpc_authority_issuer
    USING (true)
    WITH CHECK (true);

CREATE POLICY authority_proof_reservations_issuer
    ON internal_rpc_authority.authority_proof_reservations
    TO internal_rpc_authority_issuer
    USING (true)
    WITH CHECK (true);

CREATE POLICY authority_runtime_database_identities_owner
    ON internal_rpc_authority.authority_runtime_database_identities
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);

CREATE POLICY authority_runtime_database_identities_reconciler_read
    ON internal_rpc_authority.authority_runtime_database_identities
    FOR SELECT
    TO internal_rpc_authority_database_credential_reconciler
    USING (true);

CREATE POLICY database_credential_reconciliation_receipts_owner
    ON internal_rpc_authority.database_credential_reconciliation_receipts
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);

CREATE POLICY database_credential_reconciler_leases_runtime
    ON internal_rpc_authority.database_credential_reconciler_leases
    TO internal_rpc_authority_database_credential_reconciler
    USING (true)
    WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE
    ON internal_rpc_authority.authority_snapshot_watermarks
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;
GRANT SELECT, INSERT, DELETE
    ON internal_rpc_authority.authority_replay_reservations
    TO internal_rpc_authority_verifier;
GRANT SELECT, INSERT, UPDATE
    ON internal_rpc_authority.authority_proof_watermarks
    TO internal_rpc_authority_issuer;
GRANT SELECT, INSERT, UPDATE
    ON internal_rpc_authority.database_credential_reconciler_leases
    TO internal_rpc_authority_database_credential_reconciler;
GRANT SELECT
    ON internal_rpc_authority.authority_runtime_database_identities
    TO internal_rpc_authority_database_credential_reconciler;
GRANT SELECT, INSERT, DELETE
    ON internal_rpc_authority.authority_proof_reservations
    TO internal_rpc_authority_issuer;

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
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
SET search_path = pg_catalog, internal_rpc_authority
AS $function$
BEGIN
    IF NOT pg_has_role(
        session_user,
        'internal_rpc_authority_database_credential_reconciler',
        'MEMBER'
    ) THEN
        RAISE EXCEPTION 'database credential reconciler identity rejected';
    END IF;
    IF NOT (
        (requested_capability = 'PUBLISHER' AND requested_principal = 'ira_publisher_g1'
            AND requested_generation = 1 AND requested_status = 'CURRENT')
        OR
        (requested_capability = 'PUBLISHER' AND requested_principal = 'ira_publisher_g2'
            AND requested_generation = 2 AND requested_status = 'NEXT')
        OR
        (requested_capability = 'READBACK_ATTESTOR'
            AND requested_principal = 'ira_readback_attestor_g1'
            AND requested_generation = 1 AND requested_status = 'CURRENT')
        OR
        (requested_capability = 'READBACK_ATTESTOR'
            AND requested_principal = 'ira_readback_attestor_g2'
            AND requested_generation = 2 AND requested_status = 'NEXT')
    ) THEN
        RAISE EXCEPTION 'database credential registry tuple rejected';
    END IF;
    INSERT INTO internal_rpc_authority.database_credential_reconciliation_receipts (
        request_id,
        canonical_request_digest_sha256
    )
    VALUES (requested_request_id, requested_registered_set_digest_sha256)
    ON CONFLICT (request_id) DO UPDATE
    SET canonical_request_digest_sha256 =
        internal_rpc_authority.database_credential_reconciliation_receipts.canonical_request_digest_sha256
    WHERE internal_rpc_authority.database_credential_reconciliation_receipts.canonical_request_digest_sha256 =
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
        reconciled_at
    )
    VALUES (
        requested_capability,
        requested_principal,
        requested_generation,
        requested_status,
        requested_registered_set_digest_sha256,
        clock_timestamp()
    )
    ON CONFLICT (capability, generation) DO UPDATE
    SET principal = EXCLUDED.principal,
        lifecycle_status = EXCLUDED.lifecycle_status,
        registered_set_digest_sha256 = EXCLUDED.registered_set_digest_sha256,
        reconciled_at = EXCLUDED.reconciled_at
    WHERE internal_rpc_authority.authority_runtime_database_identities.principal =
            EXCLUDED.principal
      AND internal_rpc_authority.authority_runtime_database_identities.lifecycle_status =
            EXCLUDED.lifecycle_status;
    RETURN FOUND;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    requested_capability text,
    requested_principal text,
    requested_generation bigint,
    requested_request_id uuid,
    requested_registered_set_digest_sha256 text
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority
AS $function$
BEGIN
    IF NOT pg_has_role(
        session_user,
        'internal_rpc_authority_database_credential_reconciler',
        'MEMBER'
    ) THEN
        RAISE EXCEPTION 'database credential reconciler identity rejected';
    END IF;
    INSERT INTO internal_rpc_authority.database_credential_reconciliation_receipts (
        request_id,
        canonical_request_digest_sha256
    )
    VALUES (requested_request_id, requested_registered_set_digest_sha256)
    ON CONFLICT (request_id) DO UPDATE
    SET canonical_request_digest_sha256 =
        internal_rpc_authority.database_credential_reconciliation_receipts.canonical_request_digest_sha256
    WHERE internal_rpc_authority.database_credential_reconciliation_receipts.canonical_request_digest_sha256 =
        EXCLUDED.canonical_request_digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'database credential idempotency conflict';
    END IF;
    UPDATE internal_rpc_authority.authority_runtime_database_identities
    SET lifecycle_status = 'RETIRED',
        registered_set_digest_sha256 = requested_registered_set_digest_sha256,
        retired_at = clock_timestamp()
    WHERE capability = requested_capability
      AND principal = requested_principal
      AND generation = requested_generation
      AND lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS')
      AND registered_set_digest_sha256 = requested_registered_set_digest_sha256;
    RETURN FOUND;
END
$function$;
-- +goose StatementEnd

ALTER FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
    text, text, bigint, text, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    text, text, bigint, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;

REVOKE ALL ON ALL TABLES IN SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE CREATE ON SCHEMA internal_rpc_authority FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.reconcile_runtime_database_identity(
    text, text, bigint, text, uuid, text
) TO internal_rpc_authority_database_credential_reconciler;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.retire_runtime_database_identity(
    text, text, bigint, uuid, text
) TO internal_rpc_authority_database_credential_reconciler;

ALTER TABLE internal_rpc_authority.authority_snapshot_watermarks
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_replay_reservations
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_proof_watermarks
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_proof_reservations
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_runtime_database_identities
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.database_credential_reconciliation_receipts
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.database_credential_reconciler_leases
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_snapshot_history
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_restore_fences
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_rotation_intents
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_intents
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
    OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
    OWNER TO internal_rpc_authority_readback_owner;
ALTER SCHEMA internal_rpc_authority OWNER TO internal_rpc_authority_readback_owner;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
