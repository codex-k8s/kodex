-- +goose Up
RESET ROLE;

GRANT internal_rpc_authority_publisher,
      internal_rpc_authority_readback_attestor
TO internal_rpc_authority_credential_lifecycle_definer
WITH INHERIT FALSE, SET FALSE, ADMIN TRUE;

REVOKE internal_rpc_authority_publisher
FROM ira_publisher_g1,
     ira_publisher_g2,
     ira_publisher_g3,
     ira_publisher_g4,
     ira_publisher_g5
GRANTED BY CURRENT_USER;

REVOKE internal_rpc_authority_readback_attestor
FROM ira_readback_attestor_g1,
     ira_readback_attestor_g2,
     ira_readback_attestor_g3,
     ira_readback_attestor_g4,
     ira_readback_attestor_g5
GRANTED BY CURRENT_USER;

-- +goose StatementBegin
DO $readback$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS granted_role
          ON granted_role.oid = membership.roleid
        JOIN pg_catalog.pg_roles AS lifecycle_definer
          ON lifecycle_definer.oid = membership.member
        WHERE granted_role.rolname IN (
            'internal_rpc_authority_publisher',
            'internal_rpc_authority_readback_attestor'
        )
          AND lifecycle_definer.rolname =
              'internal_rpc_authority_credential_lifecycle_definer'
          AND membership.admin_option
          AND NOT membership.inherit_option
          AND NOT membership.set_option
        GROUP BY lifecycle_definer.rolname
        HAVING count(DISTINCT granted_role.rolname) = 2
    ) THEN
        RAISE EXCEPTION 'database credential capability administration is incomplete';
    END IF;
END
$readback$;
-- +goose StatementEnd

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
