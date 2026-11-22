-- +goose Up

-- Capture worker получает собственный issuer LOGIN и записи RLS только для своего workload.
RESET ROLE;
CREATE ROLE ira_artifact_retention_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
GRANT internal_rpc_authority_issuer TO ira_artifact_retention_issuer_g1
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;
SET ROLE internal_rpc_authority_owner;
GRANT CONNECT ON DATABASE internal_rpc_authority TO ira_artifact_retention_issuer_g1;
SET ROLE internal_rpc_authority_readback_owner;
INSERT INTO internal_rpc_authority.authority_workload_database_identities
    (principal, workload_id, capability, generation, lifecycle_status)
VALUES ('ira_artifact_retention_issuer_g1', 'artifact-retention', 'ISSUER', 1, 'CURRENT');
RESET ROLE;
