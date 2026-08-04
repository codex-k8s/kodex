-- name: ContinuationClaim
WITH candidate AS (
    SELECT effect.invocation_id
      FROM integration_gateway.continuation_effects AS effect
     WHERE effect.tenant_id = @tenant_id AND effect.project_id = @project_id
       AND effect.action <> 'NONE' AND effect.available_at <= clock_timestamp()
       AND (effect.continuation_id <> '' OR effect.application_grant_expires_at > clock_timestamp())
       AND effect.attempts < 32
       AND (effect.lease_expires_at IS NULL OR effect.lease_expires_at <= clock_timestamp())
     ORDER BY effect.available_at, effect.invocation_id
     LIMIT 1
     FOR UPDATE SKIP LOCKED
), claimed AS (
    UPDATE integration_gateway.continuation_effects AS effect SET
        lease_id = @lease_id,
        lease_fence = effect.lease_fence + 1,
        lease_expires_at = clock_timestamp()
            + @lease_duration_milliseconds * interval '1 millisecond',
        attempts = effect.attempts + 1,
        updated_at = clock_timestamp()
      FROM candidate
     WHERE effect.invocation_id = candidate.invocation_id
    RETURNING effect.*
)
SELECT claimed.payload, claimed.action, claimed.desired_action,
       claimed.continuation_id, claimed.continuation_version,
       claimed.continuation_fence, claimed.approval_state,
       claimed.execution_state, claimed.continuation_state,
       claimed.application_grant_expires_at, claimed.available_at,
       claimed.lease_id, claimed.lease_fence, claimed.lease_expires_at,
       claimed.attempts, invocation.payload, approval.payload,
       attempt.payload, result.payload
  FROM claimed
  JOIN integration_gateway.invocations AS invocation
    ON invocation.invocation_id = claimed.invocation_id
  JOIN integration_gateway.approvals AS approval
    ON approval.invocation_id = claimed.invocation_id
  LEFT JOIN LATERAL (
      SELECT payload FROM integration_gateway.execution_attempts
       WHERE invocation_id = claimed.invocation_id
       ORDER BY attempt_number DESC LIMIT 1
  ) AS attempt ON true
  LEFT JOIN integration_gateway.results AS result
    ON result.invocation_id = claimed.invocation_id
