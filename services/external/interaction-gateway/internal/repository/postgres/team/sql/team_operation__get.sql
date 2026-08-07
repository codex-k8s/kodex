-- name: team_operation__get :one
-- params: @arg1,@arg2,@arg3,@arg4
SELECT operation_id::text, organization_id::text, project_id::text, actor_id::text,
       idempotency_key::text, request_sha256, provider_correlation::text, display_name, slug, state,
       COALESCE(selector_id::text, ''), provider_team_id, provider_status,
       provider_snapshot_sha256, provider_causality_sha256, provider_receipt_sha256,
       COALESCE(provider_generation, 0), failure_code, fence,
       COALESCE(effect_started_at, 'epoch'::timestamptz), retry_not_before, recovery_deadline,
       created_at, updated_at, COALESCE(provider_created_at, 'epoch'::timestamptz),
       COALESCE(provider_updated_at, 'epoch'::timestamptz),
       COALESCE(provider_observed_at, 'epoch'::timestamptz), false
FROM interaction_gateway_team_operations
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid
  AND actor_id = @arg3::uuid AND operation_id = @arg4::uuid;
