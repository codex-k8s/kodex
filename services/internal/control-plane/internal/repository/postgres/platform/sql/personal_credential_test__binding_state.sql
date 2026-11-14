-- name: personal_credential_test__binding_state :exec
UPDATE control_plane.integration_personal_credentials
SET state=@state,version=@version,
 verified_credential_revision_id=CASE WHEN @state='READY' THEN credential_revision_id ELSE NULL END,
 verified_definition_digest=NULLIF(@definition_digest,''),verified_configuration_digest=NULLIF(@configuration_digest,''),
 verified_capabilities=@confirmed::text[],denied_capabilities=@denied::text[],observed_permission_profiles=@profiles::text[],expires_at=@expires_at,github_discovery=NULLIF(@github_discovery::jsonb,'null'::jsonb)
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid
 AND version=@expected_version AND generation=@generation
