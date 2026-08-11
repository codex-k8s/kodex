-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;

DROP POLICY IF EXISTS authority_runtime_database_identities_publisher_read
    ON internal_rpc_authority.authority_runtime_database_identities;
CREATE POLICY authority_runtime_database_identities_publisher_read
    ON internal_rpc_authority.authority_runtime_database_identities
    FOR SELECT
    TO internal_rpc_authority_publisher
    USING (
        capability = 'PUBLISHER'
        AND principal = session_user
        AND lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS')
    );

GRANT SELECT
    ON internal_rpc_authority.authority_runtime_database_identities
    TO internal_rpc_authority_publisher;

RESET ROLE;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
