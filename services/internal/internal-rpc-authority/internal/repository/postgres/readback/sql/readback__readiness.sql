-- name: readback__readiness :one
SELECT
    pg_catalog.has_table_privilege(
        pg_catalog.session_user,
        'internal_rpc_authority.authority_readback_intents',
        'SELECT'
    )
    AND pg_catalog.has_table_privilege(
        pg_catalog.session_user,
        'internal_rpc_authority.authority_readback_attestation_challenges',
        'SELECT,INSERT,UPDATE'
    )
    AND pg_catalog.has_table_privilege(
        pg_catalog.session_user,
        'internal_rpc_authority.authority_readback_attestation_receipts',
        'SELECT,INSERT'
    )
    AND internal_rpc_authority.runtime_restore_fence_allows_work();
