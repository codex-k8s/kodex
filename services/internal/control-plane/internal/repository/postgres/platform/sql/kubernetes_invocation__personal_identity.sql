-- name: kubernetes_invocation__personal_identity :one
SELECT identity.id::text
FROM control_plane.integration_personal_credentials binding
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=binding.broker_identity_revision_id
 AND identity.binding_id=binding.id AND identity.organization_id=binding.organization_id AND identity.subject_id=binding.subject_id
WHERE binding.organization_id=@organization_id::uuid AND binding.id=@binding_id::uuid
 AND binding.credential_kind='BROKER_IDENTITY' AND binding.broker_purpose='KUBERNETES_TARGET' AND binding.credential_revision_id IS NULL
 AND binding.state='READY' AND binding.generation=@generation AND identity.ref=@identity_ref AND identity.revision=@revision
 AND identity.purpose='KUBERNETES_TARGET' AND identity.target_ref=@target_ref;
