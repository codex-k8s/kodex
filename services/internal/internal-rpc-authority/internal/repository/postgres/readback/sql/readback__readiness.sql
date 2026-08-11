-- name: readback__readiness :one
SELECT
    current_user = 'internal_rpc_authority_readback_attestor'
    AND session_user <> current_user
    AND
    pg_catalog.has_table_privilege(
        current_user,
        'internal_rpc_authority.authority_readback_intents',
        'SELECT'
    )
    AND pg_catalog.has_table_privilege(
        current_user,
        'internal_rpc_authority.authority_readback_attestation_challenges',
        'SELECT'
    )
    AND pg_catalog.has_table_privilege(
        current_user,
        'internal_rpc_authority.authority_readback_attestation_receipts',
        'SELECT'
    )
    AND NOT pg_catalog.has_table_privilege(
        current_user,
        'internal_rpc_authority.authority_readback_attestation_challenges',
        'INSERT,UPDATE,DELETE'
    )
    AND NOT pg_catalog.has_table_privilege(
        current_user,
        'internal_rpc_authority.authority_readback_attestation_receipts',
        'INSERT,UPDATE,DELETE'
    )
    AND pg_catalog.has_function_privilege(
        current_user,
        'internal_rpc_authority.issue_authority_readback_attestation_challenge(uuid,uuid,uuid,text,text,uuid,text,uuid,text)',
        'EXECUTE'
    )
    AND pg_catalog.has_function_privilege(
        current_user,
        'internal_rpc_authority.consume_authority_readback_attestation_challenge(uuid,uuid,uuid,text,bigint,uuid,text)',
        'EXECUTE'
    )
    AND internal_rpc_authority.runtime_restore_fence_allows_work();
