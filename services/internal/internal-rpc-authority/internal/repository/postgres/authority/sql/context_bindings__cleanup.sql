-- name: context_bindings__cleanup :one
SELECT internal_rpc_authority.cleanup_issued_context_bindings(@caller_workload_id);
