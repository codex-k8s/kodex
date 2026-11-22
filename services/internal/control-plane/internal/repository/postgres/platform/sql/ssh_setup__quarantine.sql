-- name: ssh_setup__quarantine :exec
WITH quarantine AS (
 INSERT INTO control_plane.ssh_identity_quarantines(organization_id,identity_revision_id,setup_id,reason_code,recorded_at)
 VALUES(@organization_id::uuid,@identity_id::uuid,@setup_id::uuid,'CONTRADICTORY_MATERIAL',clock_timestamp())
 ON CONFLICT(identity_revision_id) DO NOTHING RETURNING identity_revision_id
), personal AS (
 UPDATE control_plane.integration_personal_credentials SET state='REVOKED',version=version+1,generation=generation+1,
 verified_broker_identity_revision_id=NULL,verified_credential_revision_id=NULL,verified_definition_digest=NULL,
 verified_configuration_digest=NULL,verified_capabilities='{}',denied_capabilities='{}',observed_permission_profiles='{}',expires_at=NULL
 WHERE organization_id=@organization_id::uuid AND connection_id=@connection_id::uuid AND broker_identity_revision_id=@identity_id::uuid
 RETURNING id
)
UPDATE control_plane.integration_connections SET state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,last_test_summary='',last_tested_at=NULL,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@connection_id::uuid AND broker_identity_revision_id=@identity_id::uuid;
