-- name: kubernetes_setup__private_permit :one
SELECT setup.id::text,setup.organization_id::text,organization.ref,actor.ref,connection.ref,
 identity.id::text,identity.ref,identity.subject_id::text,coalesce(identity.binding_id::text,''),coalesce(binding.ref,''),
 setup.registry_revision,setup.target_ref,setup.target_revision,setup.expected_connection_version,
 setup.expected_binding_version,setup.expected_binding_generation,setup.definition_digest,setup.configuration_digest,
 setup.snapshot_sha256,setup.content_sha256,setup.content_bytes,setup.expires_at,clock_timestamp(),
 setup.ref,connection.id::text,identity.revision,identity.credential_policy,setup.state,setup.version,
 actor.id::text,actor.active,connection.lifecycle_state
FROM control_plane.kubernetes_identity_setups setup
JOIN control_plane.organizations organization ON organization.id=setup.organization_id
JOIN control_plane.subjects actor ON actor.id=setup.actor_id AND actor.organization_id=setup.organization_id AND actor.kind='USER'
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id AND connection.organization_id=setup.organization_id
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
 AND identity.organization_id=setup.organization_id AND identity.connection_id=setup.connection_id AND identity.purpose='KUBERNETES_TARGET'
LEFT JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
 AND binding.organization_id=identity.organization_id AND binding.connection_id=identity.connection_id AND binding.subject_id=identity.subject_id
WHERE setup.ref=@setup_ref AND setup.permit_sha256=@permit_digest;
