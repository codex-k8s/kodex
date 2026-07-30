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
