-- name: ssh_invocation__identity :one
SELECT identity.id::text,identity.subject_id::text
FROM control_plane.integration_connections connection
JOIN control_plane.integration_broker_identity_revisions identity ON identity.organization_id=connection.organization_id
 AND identity.connection_id=connection.id AND identity.purpose='SSH_TARGET' AND identity.credential_policy=connection.credential_policy
JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=identity.organization_id AND subject.active
JOIN control_plane.ssh_identity_material_revisions material ON material.identity_revision_id=identity.id
 AND material.organization_id=identity.organization_id AND material.connection_id=connection.id
JOIN control_plane.ssh_identity_upload_observations observation ON observation.attempt_id=material.attempt_id
 AND observation.organization_id=identity.organization_id AND observation.outcome='KNOWN'
LEFT JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
 AND binding.organization_id=identity.organization_id AND binding.connection_id=connection.id
WHERE connection.organization_id=@organization_id::uuid AND connection.id=@connection_id::uuid
 AND control_plane.integration_connection_configuration_ready(connection.id)
 AND connection.credential_revision_id IS NULL AND identity.ref=@identity_ref AND identity.revision=@revision
 AND identity.target_ref=connection.public_configuration->>'target_ref'
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_ack_rejections rejection WHERE rejection.identity_revision_id=identity.id)
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_quarantines quarantine WHERE quarantine.identity_revision_id=identity.id)
 AND ((connection.credential_policy='INITIATOR_REQUIRED' AND subject.kind='USER' AND subject.id=@root_actor_id::uuid
   AND binding.id=NULLIF(@binding_id,'')::uuid AND binding.subject_id=subject.id AND binding.generation=@generation
   AND binding.credential_kind='BROKER_IDENTITY' AND binding.broker_purpose='SSH_TARGET' AND binding.state='READY'
   AND binding.broker_identity_revision_id=identity.id AND binding.verified_broker_identity_revision_id=identity.id
   AND binding.credential_revision_id IS NULL AND binding.verified_credential_revision_id IS NULL
   AND (binding.expires_at IS NULL OR binding.expires_at>clock_timestamp()))
  OR (connection.credential_policy='PROJECT_SHARED' AND connection.state='CONNECTED' AND subject.kind='SERVICE' AND identity.binding_id IS NULL
   AND subject.ref=connection.public_configuration->>'service_subject_ref' AND connection.broker_identity_revision_id=identity.id
   AND @binding_id::text='' AND @generation::bigint=0));
