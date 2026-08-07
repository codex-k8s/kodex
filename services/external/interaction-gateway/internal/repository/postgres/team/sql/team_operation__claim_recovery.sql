-- name: team_operation__claim_recovery :one
-- params: @arg1,@arg2,@arg3
WITH expired AS (
    UPDATE interaction_gateway_team_operations
    SET state = 'REPAIR_REQUIRED', failure_code = 'RECOVERY_TIMEOUT',
        lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
        updated_at = clock_timestamp()
    WHERE state IN ('EFFECT_PENDING', 'AMBIGUOUS') AND recovery_deadline <= clock_timestamp()
    RETURNING organization_id, project_id, operation_id
), expired_fences AS (
    UPDATE interaction_gateway_team_create_fences AS fence
    SET state = 'REPAIR_REQUIRED', updated_at = clock_timestamp()
    FROM expired
    WHERE fence.organization_id = expired.organization_id
      AND fence.project_id = expired.project_id
      AND fence.operation_id = expired.operation_id
    RETURNING fence.operation_id
), candidate AS (
    SELECT operation_id
    FROM interaction_gateway_team_operations
    WHERE state IN ('PENDING', 'EFFECT_PENDING', 'AMBIGUOUS')
      AND retry_not_before <= clock_timestamp()
      AND recovery_deadline > clock_timestamp()
      AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
    ORDER BY retry_not_before, created_at, operation_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE interaction_gateway_team_operations AS operation
SET fence = operation.fence + 1,
    lease_owner = @arg1::text,
    lease_token_sha256 = @arg2::text,
    lease_expires_at = clock_timestamp() + @arg3::interval,
    updated_at = clock_timestamp()
FROM candidate
WHERE operation.operation_id = candidate.operation_id
RETURNING operation.operation_id::text, operation.organization_id::text,
          operation.project_id::text, operation.actor_id::text,
          operation.idempotency_key::text, operation.request_sha256, operation.provider_correlation::text,
          operation.display_name, operation.slug, operation.state,
          COALESCE(operation.selector_id::text, ''), operation.provider_team_id,
          operation.provider_status, operation.provider_snapshot_sha256, operation.provider_causality_sha256,
          operation.provider_receipt_sha256, COALESCE(operation.provider_generation, 0),
          operation.failure_code, operation.fence,
          COALESCE(operation.effect_started_at, 'epoch'::timestamptz),
          operation.retry_not_before, operation.recovery_deadline, operation.created_at, operation.updated_at,
          COALESCE(operation.provider_created_at, 'epoch'::timestamptz),
          COALESCE(operation.provider_updated_at, 'epoch'::timestamptz),
          COALESCE(operation.provider_observed_at, 'epoch'::timestamptz),
          true;
