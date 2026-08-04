-- name: LifecycleExpire
WITH authority_changed AS (
    SELECT invocation.invocation_id, invocation.tenant_id, invocation.project_id,
           invocation.canonical_request_hash,
           invocation.status AS previous_status,
           EXISTS (
               SELECT 1 FROM integration_gateway.execution_attempts AS attempt
                WHERE attempt.invocation_id = invocation.invocation_id
                  AND attempt.finished_at IS NULL
                  AND attempt.provider_dispatched_at IS NOT NULL
           ) AS provider_dispatched
      FROM integration_gateway.invocations AS invocation
      JOIN integration_gateway.connections AS connection
        ON connection.connection_id = invocation.connection_id
       AND connection.tenant_id = invocation.tenant_id
       AND connection.project_id = invocation.project_id
      JOIN integration_gateway.grants AS grant
        ON grant.grant_id = invocation.grant_id
       AND grant.tenant_id = invocation.tenant_id
       AND grant.project_id = invocation.project_id
      JOIN integration_gateway.continuation_effects AS effect
        ON effect.invocation_id = invocation.invocation_id
     WHERE invocation.status IN ('PENDING_APPROVAL', 'APPROVED', 'EXECUTING')
       AND (connection.status <> 'VALID'
         OR connection.generation <> invocation.connection_generation
         OR grant.status <> 'ACTIVE'
         OR grant.generation <> invocation.grant_generation
         OR (effect.continuation_id = '' AND grant.expires_at <= clock_timestamp())
         OR (effect.continuation_id = '' AND effect.attempts >= 32)
         OR EXISTS (
             SELECT 1
               FROM jsonb_array_elements(invocation.payload->'PinnedConnection'->'CredentialBindingRefs') AS binding
              WHERE binding->>'ExpiresAt' IS NOT NULL
                AND (binding->>'ExpiresAt')::timestamptz <= clock_timestamp()
         ))
     ORDER BY invocation.updated_at, invocation.invocation_id
     LIMIT @limit
     FOR UPDATE OF invocation, connection, grant, effect SKIP LOCKED
), authority_terminal_invocations AS (
    UPDATE integration_gateway.invocations AS invocation SET
        status = CASE WHEN changed.provider_dispatched THEN 'UNKNOWN' ELSE 'CANCELLED' END,
        updated_at = clock_timestamp(),
        payload = jsonb_set(invocation.payload, '{Status}',
            to_jsonb(CASE WHEN changed.provider_dispatched THEN 'UNKNOWN' ELSE 'CANCELLED' END::text), false)
      FROM authority_changed AS changed
     WHERE invocation.invocation_id = changed.invocation_id
    RETURNING invocation.invocation_id
), authority_terminal_approvals AS (
    UPDATE integration_gateway.approvals AS approval SET
        status = 'CANCELLED', decided_at = clock_timestamp(),
        payload = jsonb_set(approval.payload, '{Status}', '"CANCELLED"'::jsonb, false)
     WHERE approval.invocation_id IN (SELECT invocation_id FROM authority_terminal_invocations)
       AND approval.status IN ('PENDING', 'APPROVED')
    RETURNING approval.invocation_id
), authority_terminal_attempts AS (
    UPDATE integration_gateway.execution_attempts AS attempt SET
        outcome = CASE WHEN changed.provider_dispatched THEN 'UNKNOWN' ELSE 'CANCELLED' END,
        finished_at = clock_timestamp(),
        payload = jsonb_set(
            jsonb_set(attempt.payload, '{Outcome}',
                to_jsonb(CASE WHEN changed.provider_dispatched THEN 'UNKNOWN' ELSE 'CANCELLED' END::text), false),
            '{FinishedAt}', to_jsonb(clock_timestamp()), false
        )
      FROM authority_changed AS changed
     WHERE attempt.invocation_id = changed.invocation_id
       AND attempt.finished_at IS NULL
    RETURNING attempt.invocation_id, attempt.attempt_id, attempt.tenant_id,
              attempt.project_id, attempt.finished_at
), authority_terminal_results AS (
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
                   attempt.invocation_id, attempt.attempt_id, 'UNKNOWN'
               ),
               'ProviderReceipt', '',
               'DeliveryVersion', 1,
               'DeliveryFence', 1,
               'AcknowledgedAt', NULL,
               'CompletedAt', attempt.finished_at
           ), attempt.finished_at
      FROM authority_terminal_attempts AS attempt
      JOIN authority_changed AS changed ON changed.invocation_id = attempt.invocation_id
     WHERE changed.provider_dispatched
    ON CONFLICT (invocation_id) DO NOTHING
    RETURNING invocation_id
), authority_terminal_continuations AS (
    UPDATE integration_gateway.continuation_effects AS effect SET
        desired_action = CASE WHEN effect.continuation_id = '' THEN 'NONE'
            WHEN changed.provider_dispatched THEN 'FAIL' ELSE 'CANCEL' END,
        action = CASE
            WHEN effect.continuation_id = '' THEN 'NONE'
            WHEN changed.provider_dispatched THEN 'FAIL'
            ELSE 'CANCEL'
        END,
        lease_id = '', lease_expires_at = NULL,
        available_at = clock_timestamp(), updated_at = clock_timestamp()
      FROM authority_changed AS changed
     WHERE effect.invocation_id = changed.invocation_id
    RETURNING effect.invocation_id
), expired_sessions AS (
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
               'DeliveryVersion', 1,
               'DeliveryFence', 1,
               'AcknowledgedAt', NULL,
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
           'system:integration-gateway-lifecycle', 'authority.revoke',
           'TOOL_INVOCATION', invocation_id, canonical_request_hash,
           CASE WHEN provider_dispatched THEN 'UNKNOWN' ELSE 'CANCELLED' END,
           CASE WHEN previous_status IN ('PENDING_APPROVAL', 'APPROVED')
                AND NOT provider_dispatched THEN 'AUTHORITY_OR_HANDOFF_REVOKED'
                ELSE 'AUTHORITY_CHANGED' END, clock_timestamp()
      FROM authority_changed
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
     + (SELECT count(*) FROM authority_terminal_invocations)
     + (SELECT count(*) FROM expired_invocations)
     + (SELECT count(*) FROM expired_approved_invocations)
     + (SELECT count(*) FROM abandoned_results)
