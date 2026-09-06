-- name: runtime_claimexecution_count_active_provider_leases :one
SELECT control_plane.provider_account_active_executions($2::uuid,$1::uuid);
