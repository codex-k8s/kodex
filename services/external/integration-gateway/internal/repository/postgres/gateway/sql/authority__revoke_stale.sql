-- name: AuthorityRevokeStale
WITH stale_invocations AS (
    UPDATE integration_gateway.invocations SET
        status = 'CANCELLED', updated_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"CANCELLED"'::jsonb, false)
     WHERE tenant_id = @tenant_id AND project_id = @project_id
       AND connection_id = @connection_id AND connection_generation < @generation
       AND status IN ('PENDING_APPROVAL', 'APPROVED')
    RETURNING invocation_id, canonical_request_hash
), stale_approvals AS (
    UPDATE integration_gateway.approvals SET
        status = 'CANCELLED', decided_at = clock_timestamp(),
        payload = jsonb_set(payload, '{Status}', '"CANCELLED"'::jsonb, false)
     WHERE invocation_id IN (SELECT invocation_id FROM stale_invocations)
       AND status = 'PENDING'
    RETURNING invocation_id
), stale_audit AS (
    INSERT INTO integration_gateway.audit_events (
        audit_id, tenant_id, project_id, actor_id, action, resource_kind,
        resource_id, request_hash, outcome, reason_code, occurred_at
    )
    SELECT integration_gateway_extensions.gen_random_uuid()::text, @tenant_id, @project_id,
           'system:integration-gateway', 'invocation.cancel', 'TOOL_INVOCATION',
           invocation_id, canonical_request_hash, 'CANCELLED', 'AUTHORITY_GENERATION_CHANGED', clock_timestamp()
      FROM stale_invocations
    RETURNING audit_id
)
SELECT count(*) FROM stale_invocations
