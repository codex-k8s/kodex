-- name: publisher__readiness :one
SELECT
    current_user = 'internal_rpc_authority_publisher'
    AND session_user <> current_user
    AND internal_rpc_authority.runtime_restore_fence_allows_work()
    AND pg_catalog.has_table_privilege(
        current_user,
        'internal_rpc_authority.authority_publisher_delivery_receipts',
        'SELECT,INSERT'
    )
    AND EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_runtime_database_identities
        WHERE capability = 'PUBLISHER'
          AND principal = session_user
          AND lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS')
    );
