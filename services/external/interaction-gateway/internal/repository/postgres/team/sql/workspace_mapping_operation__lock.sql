SELECT operation_id::text, organization_id::text, project_id::text, actor_id::text,
       action, idempotency_key::text, request_sha256, COALESCE(mapping_id::text, ''),
       expected_mapping_version, expected_mapping_generation, display_name,
       selector_id::text, provider_team_id, provider_status, provider_snapshot_sha256,
       provider_created_at, provider_updated_at, provider_observed_at,
       effect_generation, receipt_id::text, state, failure_code, fence,
       retry_not_before, created_at, updated_at,
       COALESCE(result_mapping_id::text, ''), COALESCE(result_mapping_version, 0), COALESCE(result_mapping_generation, 0),
       COALESCE(result_provider_effect_version, 0), COALESCE(result_provider_effect_generation, 0),
       COALESCE(result_provider_observed_at, 'epoch'::timestamptz),
       COALESCE(result_updated_at, 'epoch'::timestamptz),
       lease_expires_at IS NOT NULL AND lease_expires_at > clock_timestamp()
FROM interaction_gateway_workspace_mapping_operations
WHERE operation_id = $1::uuid
FOR UPDATE;
