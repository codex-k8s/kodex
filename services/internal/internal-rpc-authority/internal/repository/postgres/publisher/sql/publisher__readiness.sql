-- name: publisher__readiness :one
SELECT
    pg_catalog.current_user = 'internal_rpc_authority_publisher'
    AND pg_catalog.session_user <> pg_catalog.current_user
    AND pg_catalog.has_table_privilege(
        pg_catalog.current_user,
        'internal_rpc_authority.authority_publisher_delivery_receipts',
        'SELECT,INSERT'
    )
    AND EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_runtime_database_identities
        WHERE capability = 'PUBLISHER'
          AND principal = pg_catalog.session_user
          AND lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS')
    );
