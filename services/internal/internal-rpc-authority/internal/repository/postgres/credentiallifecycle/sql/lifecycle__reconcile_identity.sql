-- name: lifecycle__reconcile_identity :one
SELECT internal_rpc_authority.reconcile_runtime_database_identity(
    @capability,
    @principal,
    @generation,
    @status,
    @request_id,
    @registered_set_digest_sha256
);
