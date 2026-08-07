-- name: LegacyConfigurationCutoverMarkMigrated
UPDATE control_plane.legacy_configuration_cutovers
SET state = 'MIGRATED', block_code = NULL, manual_action = NULL,
    result_agent_version = @result_agent_version,
    result_agent_sha256 = @result_agent_sha256,
    resolved_at = @resolved_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @actor_id::uuid
  AND legacy_role_id = @legacy_role_id::uuid
  AND state = 'BLOCKED';
