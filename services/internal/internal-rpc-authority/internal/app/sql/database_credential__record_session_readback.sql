-- name: internal_rpc_authority__database_credential__record_session_readback :one
SELECT internal_rpc_authority.record_database_credential_session_readback($1, $2::uuid)
