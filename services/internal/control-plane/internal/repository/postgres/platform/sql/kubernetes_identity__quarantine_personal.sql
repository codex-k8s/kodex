-- name: kubernetes_identity__quarantine_personal :exec
UPDATE control_plane.integration_personal_credentials
SET state='REVOKED',version=version+1,generation=generation+1,
 verified_credential_revision_id=NULL,verified_broker_identity_revision_id=NULL,
 verified_definition_digest=NULL,verified_configuration_digest=NULL,verified_capabilities='{}',denied_capabilities='{}',
 observed_permission_profiles='{}',expires_at=NULL
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid AND connection_id=@connection_id::uuid
 AND subject_id=@subject_id::uuid AND credential_kind='BROKER_IDENTITY' AND broker_purpose='KUBERNETES_TARGET'
 AND broker_identity_revision_id=@identity_id::uuid AND state<>'REVOKED';
