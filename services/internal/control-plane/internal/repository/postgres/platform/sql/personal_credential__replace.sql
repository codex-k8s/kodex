-- name: personal_credential__replace :exec
UPDATE control_plane.integration_personal_credentials
SET credential_revision_id = @revision_id::uuid, version = @version, generation = @generation,
    state = 'CHECK_REQUIRED', verified_credential_revision_id = NULL, verified_definition_digest = NULL,
    verified_configuration_digest = NULL, verified_capabilities = '{}', denied_capabilities = '{}', observed_permission_profiles = '{}', expires_at = NULL
WHERE id = @binding_id::uuid AND organization_id = @organization_id::uuid AND subject_id = @subject_id::uuid
  AND version = @expected_version
