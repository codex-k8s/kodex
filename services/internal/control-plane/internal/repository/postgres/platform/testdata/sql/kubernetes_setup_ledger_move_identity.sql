-- Только synthetic metadata: не утверждает material/account eligibility новой RI.
UPDATE control_plane.integration_personal_credentials binding
SET broker_identity_revision_id=identity.id,version=binding.version+1,generation=binding.generation+1,state='CHECK_REQUIRED',
 verified_broker_identity_revision_id=NULL,verified_definition_digest=NULL,verified_configuration_digest=NULL,
 verified_capabilities='{}',denied_capabilities='{}',observed_permission_profiles='{}',expires_at=NULL
FROM control_plane.integration_broker_identity_revisions identity
WHERE identity.id=@identity_id::uuid AND identity.binding_id=binding.id AND identity.organization_id=binding.organization_id;
