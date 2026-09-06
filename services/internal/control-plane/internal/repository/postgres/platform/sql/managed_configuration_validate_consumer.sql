-- name: managed_configuration_validate_consumer :one
SELECT CASE @consumer_kind
	WHEN 'AGENT_CONTINUATION' THEN EXISTS (SELECT 1 FROM control_plane.agents value WHERE value.organization_id = @organization_id::uuid AND value.ref = @consumer_ref AND value.project_id IS NOT DISTINCT FROM @project_id::uuid)
    WHEN 'AGENT' THEN EXISTS (SELECT 1 FROM control_plane.agents value WHERE value.organization_id = @organization_id::uuid AND value.ref = @consumer_ref AND (@project_id::uuid IS NULL OR value.project_id = @project_id::uuid))
    WHEN 'WORKFLOW' THEN EXISTS (SELECT 1 FROM control_plane.workflows value WHERE value.organization_id = @organization_id::uuid AND value.ref = @consumer_ref AND (@project_id::uuid IS NULL OR value.project_id = @project_id::uuid))
    WHEN 'SCHEDULE' THEN EXISTS (SELECT 1 FROM control_plane.schedules value WHERE value.organization_id = @organization_id::uuid AND value.ref = @consumer_ref AND (@project_id::uuid IS NULL OR value.project_id = @project_id::uuid))
    WHEN 'RUNTIME_ENVIRONMENT' THEN EXISTS (SELECT 1 FROM control_plane.runtime_environment_sets value WHERE value.organization_id = @organization_id::uuid AND value.ref = @consumer_ref AND (@project_id::uuid IS NULL OR value.project_id = @project_id::uuid))
    WHEN 'INTEGRATION_CONNECTION' THEN EXISTS (SELECT 1 FROM control_plane.integration_connections value WHERE value.organization_id = @organization_id::uuid AND value.ref = @consumer_ref AND value.lifecycle_state = 'ACTIVE' AND value.definition_key = @expected_definition_key)
    WHEN 'STT_SERVICE' THEN @consumer_ref = 'stt-tts-service'
    ELSE false END;
