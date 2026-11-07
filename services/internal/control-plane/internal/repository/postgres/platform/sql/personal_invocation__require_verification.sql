-- name: personal_invocation__require_verification :exec
UPDATE control_plane.integration_personal_credentials binding
SET state='CHECK_REQUIRED',version=version+1,verified_credential_revision_id=NULL,
 verified_definition_digest=NULL,verified_configuration_digest=NULL,
 verified_capabilities='{}',denied_capabilities='{}',observed_permission_profiles='{}',expires_at=NULL
FROM control_plane.integration_personal_credential_revisions revision
WHERE binding.organization_id=@organization_id::uuid AND binding.id=@binding_id::uuid
 AND binding.generation=@binding_generation::bigint AND binding.credential_revision_id=revision.id
 AND revision.organization_id=binding.organization_id AND revision.binding_id=binding.id
 AND revision.ref=@credential_ref AND binding.state IN ('READY','CHECKING');
