-- name: LegacyConfigurationCutoverGetForUpdate
SELECT organization_id::text, project_id::text, owner_actor_id::text,
       legacy_role_id::text, legacy_role_version,
       legacy_prompt_profile_id::text, legacy_prompt_version,
       source_role_sha256, source_prompt_sha256,
       ARRAY(SELECT value::text FROM unnest(source_credential_ids) AS value ORDER BY value),
       target_role_definition_id::text, target_agent_id::text,
       target_instruction_set_id::text, target_provider_pool_id::text,
       target_agent_assignment_id::text,
       ARRAY(SELECT value::text FROM unnest(target_provider_reference_ids) AS value ORDER BY value),
       state, coalesce(block_code, ''),
       coalesce(manual_action, ''), result_agent_version,
       coalesce(result_agent_sha256, ''), created_at, resolved_at
FROM control_plane.legacy_configuration_cutovers
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @actor_id::uuid
  AND legacy_role_id = @legacy_role_id::uuid
FOR UPDATE;
