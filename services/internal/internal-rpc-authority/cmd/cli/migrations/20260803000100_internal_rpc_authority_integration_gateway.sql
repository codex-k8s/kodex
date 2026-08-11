-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;
-- Workload-local issuer integration-gateway получает собственные PostgreSQL
-- principals. Vault static roles связываются с ними по delivery registry.
RESET ROLE;
-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_integration_gateway_issuer_g1'
    ) THEN
        CREATE ROLE ira_integration_gateway_issuer_g1
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = 'ira_integration_gateway_issuer_g2'
    ) THEN
        CREATE ROLE ira_integration_gateway_issuer_g2
            LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
END
$roles$;
-- +goose StatementEnd

ALTER ROLE ira_integration_gateway_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
ALTER ROLE ira_integration_gateway_issuer_g2
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
GRANT internal_rpc_authority_issuer
    TO ira_integration_gateway_issuer_g1,
       ira_integration_gateway_issuer_g2;
SET ROLE internal_rpc_authority_owner;

-- +goose Down
-- Forward-only: credential generation и readback watermark не открываются назад.
SELECT 1;
