-- name: broker_identity__attach_personal :exec
UPDATE control_plane.integration_personal_credentials
SET broker_identity_revision_id=@identity_id::uuid,version=version+1,generation=generation+1,state='CHECK_REQUIRED',
 credential_revision_id=NULL,verified_credential_revision_id=NULL,verified_broker_identity_revision_id=NULL,
 verified_definition_digest=NULL,verified_configuration_digest=NULL,verified_capabilities='{}',denied_capabilities='{}',
 observed_permission_profiles='{}',expires_at=NULL
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid
 AND connection_id=@connection_id::uuid AND subject_id=@subject_id::uuid
 AND version=@version AND generation=@generation AND credential_kind='BROKER_IDENTITY' AND broker_purpose=@purpose
 AND clock_timestamp()<@authorized_until::timestamptz;
