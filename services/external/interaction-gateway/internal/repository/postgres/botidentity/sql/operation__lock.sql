-- name: operation__lock :one
-- params: @arg1
SELECT operation.operation_id::text, operation.organization_id::text, operation.project_id::text,
       operation.actor_id::text, operation.action, operation.idempotency_key::text,
       operation.agent_ref::text, operation.expected_agent_version, operation.predecessor_generation,
       COALESCE(operation.identity_ref::text, ''), COALESCE(operation.selector_id::text, ''),
       operation.request_sha256, operation.username, operation.display_name,
       COALESCE(operation.provider_correlation::text, ''), operation.state,
       COALESCE(operation.receipt_id::text, ''), COALESCE(operation.receipt_revision, 0),
       operation.receipt_sha256, operation.command_intent_sha256,
       COALESCE(operation.result_agent_version, 0), operation.failure_code, operation.fence,
       COALESCE(operation.effect_started_at, 'epoch'::timestamptz),
       operation.retry_not_before, operation.recovery_deadline, operation.created_at, operation.updated_at,
       COALESCE(identity.identity_ref::text, ''), COALESCE(identity.provider_object_ref::text, ''),
       COALESCE(identity.agent_ref::text, ''), COALESCE(identity.agent_stable_key, ''),
       COALESCE(identity.provider_bot_id, ''), COALESCE(identity.provider_user_id, ''),
       COALESCE(identity.provider_team_id, ''), COALESCE(identity.provider_token_id, ''),
       COALESCE(identity.credential_binding_id::text, ''), COALESCE(identity.credential_secret_ref, ''),
       COALESCE(identity.credential_secret_version, 0), COALESCE(identity.credential_sha256, ''),
       COALESCE(identity.username, ''), COALESCE(identity.display_name, ''), COALESCE(identity.status, 'UNKNOWN'),
       COALESCE(identity.provider_version, 0), COALESCE(identity.provider_generation, 0),
       COALESCE(identity.provider_snapshot_sha256, ''), COALESCE(identity.provider_causality_sha256, ''),
       COALESCE(identity.observed_at, 'epoch'::timestamptz), COALESCE(identity.created_at, 'epoch'::timestamptz),
       COALESCE(identity.updated_at, 'epoch'::timestamptz),
       COALESCE(operation.result_agent_version, 0), operation.receipt_sha256,
       CASE WHEN operation.result_agent_version IS NULL THEN 'epoch'::timestamptz ELSE operation.updated_at END,
       operation.lease_expires_at > clock_timestamp()
FROM interaction_gateway_agent_bot_operations AS operation
LEFT JOIN interaction_gateway_agent_bot_identities AS identity ON identity.identity_ref = operation.identity_ref
WHERE operation.operation_id = @arg1::uuid
FOR UPDATE OF operation;
