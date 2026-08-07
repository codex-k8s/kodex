WITH candidate AS (
    SELECT operation_id
    FROM interaction_gateway_workspace_mapping_operations
    WHERE state IN ('PENDING', 'AMBIGUOUS')
      AND retry_not_before <= clock_timestamp()
      AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
    ORDER BY retry_not_before, created_at, operation_id
    LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE interaction_gateway_workspace_mapping_operations AS operation
SET fence = operation.fence + 1,
    lease_owner = $1::text,
    lease_token_sha256 = $2::text,
    lease_expires_at = clock_timestamp() + $3::interval,
    updated_at = clock_timestamp()
FROM candidate
WHERE operation.operation_id = candidate.operation_id
RETURNING operation.operation_id::text;
