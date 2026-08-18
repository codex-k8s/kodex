-- name: workspace_mapping_operation__claim_recovery :one
-- params: @arg1,@arg2,@arg3
WITH expired AS (
    UPDATE interaction_gateway_workspace_mapping_operations
    SET state = 'REPAIR_REQUIRED', failure_code = 'RECOVERY_TIMEOUT',
        lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
        updated_at = clock_timestamp()
    WHERE state IN ('PENDING', 'AMBIGUOUS') AND recovery_deadline <= clock_timestamp()
    RETURNING organization_id, project_id, create_operation_id
), expired_fences AS (
    UPDATE interaction_gateway_team_create_fences AS fence
    SET state = 'REPAIR_REQUIRED', updated_at = clock_timestamp()
    FROM expired
    WHERE expired.create_operation_id IS NOT NULL
      AND fence.organization_id = expired.organization_id
      AND fence.project_id = expired.project_id
      AND fence.operation_id = expired.create_operation_id
    RETURNING fence.operation_id
), candidate AS (
    SELECT operation_id
    FROM interaction_gateway_workspace_mapping_operations
    WHERE state IN ('PENDING', 'AMBIGUOUS')
      AND retry_not_before <= clock_timestamp()
      AND recovery_deadline > clock_timestamp()
      AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
    ORDER BY retry_not_before, created_at, operation_id
    LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE interaction_gateway_workspace_mapping_operations AS operation
SET fence = operation.fence + 1,
    lease_owner = @arg1::text,
    lease_token_sha256 = @arg2::text,
    lease_expires_at = clock_timestamp() + @arg3::interval,
    updated_at = clock_timestamp()
FROM candidate
WHERE operation.operation_id = candidate.operation_id
RETURNING operation.operation_id::text;
