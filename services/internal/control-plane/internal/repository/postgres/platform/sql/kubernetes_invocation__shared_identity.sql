-- name: kubernetes_invocation__shared_identity :one
SELECT identity.id::text,identity.subject_id::text,identity.ref,identity.target_ref,identity.purpose,identity.revision,identity.created_at
FROM control_plane.integration_connections connection
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=connection.broker_identity_revision_id
 AND identity.organization_id=connection.organization_id AND identity.connection_id=connection.id
 AND identity.credential_policy=connection.credential_policy AND identity.binding_id IS NULL AND identity.purpose='KUBERNETES_TARGET'
JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=identity.organization_id
 AND subject.kind='SERVICE' AND subject.active AND subject.ref=connection.public_configuration->>'service_subject_ref'
WHERE connection.organization_id=@organization_id::uuid AND connection.id=@connection_id::uuid
 AND connection.credential_policy='PROJECT_SHARED' AND connection.lifecycle_state='ACTIVE' AND connection.enabled
 AND connection.state='CONNECTED' AND connection.masked_credentials_state='CONFIGURED'
 AND connection.credential_revision_id IS NULL AND identity.target_ref=connection.public_configuration->>'target_ref'
FOR SHARE OF subject;
