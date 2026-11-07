-- name: personal_credential__revoke :exec
UPDATE control_plane.integration_personal_credentials
SET version = @version, generation = @generation, state = 'REVOKED',
    verified_credential_revision_id = NULL, verified_definition_digest = NULL,
    verified_configuration_digest = NULL, verified_capabilities = '{}', denied_capabilities = '{}', observed_permission_profiles = '{}', expires_at = NULL
WHERE id = @binding_id::uuid AND organization_id = @organization_id::uuid AND subject_id = @subject_id::uuid
  AND version = @expected_version
