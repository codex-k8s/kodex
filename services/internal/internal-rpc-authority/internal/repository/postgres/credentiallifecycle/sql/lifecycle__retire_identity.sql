-- name: lifecycle__retire_identity :one
SELECT internal_rpc_authority.retire_runtime_database_identity(
    @capability,
    @principal,
    @generation,
    @request_id,
    @registered_set_digest_sha256
);
