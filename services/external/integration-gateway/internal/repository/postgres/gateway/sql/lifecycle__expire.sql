-- name: LifecycleExpire
WITH expired_sessions AS (
    UPDATE integration_gateway.transport_sessions SET status = 'EXPIRED', concurrent_requests = 0
     WHERE transport_session_id IN (
        SELECT transport_session_id FROM integration_gateway.transport_sessions
         WHERE status IN ('INITIALIZING', 'ACTIVE') AND expires_at <= clock_timestamp()
         ORDER BY expires_at LIMIT @limit FOR UPDATE SKIP LOCKED
     ) RETURNING transport_session_id, tenant_id, project_id
), expired_approvals AS (
    UPDATE integration_gateway.approvals SET status = 'EXPIRED', decided_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"EXPIRED"'::jsonb, false)
     WHERE approval_id IN (
        SELECT approval_id FROM integration_gateway.approvals
         WHERE status = 'PENDING' AND expires_at <= clock_timestamp()
         ORDER BY expires_at LIMIT @limit FOR UPDATE SKIP LOCKED
     ) RETURNING invocation_id, tenant_id, project_id, request_hash
), expired_invocations AS (
    UPDATE integration_gateway.invocations SET status = 'EXPIRED', updated_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"EXPIRED"'::jsonb, false)
     WHERE invocation_id IN (SELECT invocation_id FROM expired_approvals)
       AND status = 'PENDING_APPROVAL'
    RETURNING invocation_id, tenant_id, project_id, canonical_request_hash
), expired_approved_invocations AS (
    UPDATE integration_gateway.invocations SET status = 'EXPIRED', updated_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"EXPIRED"'::jsonb, false)
     WHERE invocation_id IN (
        SELECT invocation_id FROM integration_gateway.invocations
         WHERE status = 'APPROVED' AND expires_at <= clock_timestamp()
         ORDER BY expires_at LIMIT @limit FOR UPDATE SKIP LOCKED
    )
    RETURNING invocation_id, tenant_id, project_id, canonical_request_hash
), expired_approved_approvals AS (
    UPDATE integration_gateway.approvals SET status = 'EXPIRED', decided_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"EXPIRED"'::jsonb, false)
     WHERE invocation_id IN (SELECT invocation_id FROM expired_approved_invocations)
       AND status = 'APPROVED'
    RETURNING invocation_id
), abandoned_invocations AS (
    UPDATE integration_gateway.invocations SET status = 'UNKNOWN', updated_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"UNKNOWN"'::jsonb, false)
     WHERE invocation_id IN (
        SELECT invocation_id FROM integration_gateway.invocations
         WHERE status = 'EXECUTING' AND updated_at <= clock_timestamp() - interval '1 minute'
         ORDER BY updated_at LIMIT @limit FOR UPDATE SKIP LOCKED
     )
    RETURNING invocation_id, tenant_id, project_id, canonical_request_hash
), abandoned_attempts AS (
    UPDATE integration_gateway.execution_attempts AS attempt SET
        outcome = 'UNKNOWN', finished_at = clock_timestamp(),
        payload = jsonb_set(
            jsonb_set(attempt.payload, '{Outcome}', '"UNKNOWN"'::jsonb, false),
            '{FinishedAt}', to_jsonb(clock_timestamp()), false
        )
     WHERE attempt.invocation_id IN (SELECT invocation_id FROM abandoned_invocations)
       AND attempt.finished_at IS NULL
    RETURNING attempt.invocation_id, attempt.attempt_id, attempt.tenant_id, attempt.project_id, attempt.finished_at
), abandoned_results AS (
    INSERT INTO integration_gateway.results (
        invocation_id, tenant_id, project_id, attempt_id, status, payload, completed_at
    )
    SELECT attempt.invocation_id, attempt.tenant_id, attempt.project_id, attempt.attempt_id,
           'UNKNOWN', jsonb_build_object(
               'InvocationID', attempt.invocation_id,
               'AttemptID', attempt.attempt_id,
               'Status', 'UNKNOWN',
               'EncryptedPayload', NULL,
               'PayloadDigest', integration_gateway.result_reference_digest(
                   attempt.invocation_id,
                   attempt.attempt_id,
                   'UNKNOWN'
               ),
               'ProviderReceipt', '',
               'CompletedAt', attempt.finished_at
           ), attempt.finished_at
      FROM abandoned_attempts AS attempt
    ON CONFLICT (invocation_id) DO NOTHING
    RETURNING invocation_id
), expired_continuations AS (
    UPDATE integration_gateway.continuation_effects AS effect SET
        desired_action = 'EXPIRE',
        action = CASE WHEN effect.action = 'SUSPEND' THEN 'SUSPEND' ELSE 'EXPIRE' END,
        available_at = clock_timestamp(),
        updated_at = clock_timestamp()
     WHERE effect.invocation_id IN (
        SELECT invocation_id FROM expired_invocations
        UNION ALL
        SELECT invocation_id FROM expired_approved_invocations
     )
    RETURNING effect.invocation_id
), abandoned_continuations AS (
    UPDATE integration_gateway.continuation_effects AS effect SET
        desired_action = 'FAIL',
        action = CASE WHEN effect.action = 'BEGIN' THEN 'BEGIN' ELSE 'FAIL' END,
        available_at = clock_timestamp(),
        updated_at = clock_timestamp()
     WHERE effect.invocation_id IN (SELECT invocation_id FROM abandoned_invocations)
    RETURNING effect.invocation_id
), lifecycle_audit AS (
    INSERT INTO integration_gateway.audit_events (
        audit_id, tenant_id, project_id, actor_id, action, resource_kind,
        resource_id, request_hash, outcome, reason_code, occurred_at
    )
    SELECT integration_gateway_extensions.gen_random_uuid()::text, tenant_id, project_id,
           'system:integration-gateway-lifecycle', 'transport_session.expire',
           'TRANSPORT_SESSION', transport_session_id, '', 'EXPIRED', 'TTL', clock_timestamp()
      FROM expired_sessions
    UNION ALL
    SELECT integration_gateway_extensions.gen_random_uuid()::text, tenant_id, project_id,
           'system:integration-gateway-lifecycle', 'approval.expire',
           'TOOL_INVOCATION', invocation_id, canonical_request_hash, 'EXPIRED', 'TTL', clock_timestamp()
      FROM expired_invocations
    UNION ALL
    SELECT integration_gateway_extensions.gen_random_uuid()::text, tenant_id, project_id,
           'system:integration-gateway-lifecycle', 'invocation.expire',
           'TOOL_INVOCATION', invocation_id, canonical_request_hash, 'EXPIRED', 'TTL', clock_timestamp()
      FROM expired_approved_invocations
    UNION ALL
    SELECT integration_gateway_extensions.gen_random_uuid()::text, tenant_id, project_id,
           'system:integration-gateway-lifecycle', 'execution.recover',
           'TOOL_INVOCATION', invocation_id, canonical_request_hash, 'UNKNOWN', 'EXECUTION_LEASE_EXPIRED', clock_timestamp()
      FROM abandoned_invocations
    RETURNING audit_id
)
SELECT (SELECT count(*) FROM expired_sessions)
     + (SELECT count(*) FROM expired_invocations)
     + (SELECT count(*) FROM expired_approved_invocations)
     + (SELECT count(*) FROM abandoned_results)
