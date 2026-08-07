-- name: workspace_mapping_operation__reclaim :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_workspace_mapping_operations
SET fence = fence + 1,
    lease_owner = @arg2::text,
    lease_token_sha256 = @arg3::text,
    lease_expires_at = clock_timestamp() + @arg4::interval,
    retry_not_before = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid
  AND state IN ('PENDING', 'AMBIGUOUS')
  AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp());
