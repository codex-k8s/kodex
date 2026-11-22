-- name: ssh_setup__completion_source :one
SELECT setup.id::text,setup.snapshot_body,setup.snapshot_sha256,setup.state,setup.version,
 setup.expires_at,clock_timestamp(),coalesce(setup.authentication_sha256,''),organization.ref,actor.ref,actor.active,
 connection.lifecycle_state,connection.id::text,connection.ref,connection.organization_id::text,setup.actor_id::text,
 identity.id::text,identity.ref,identity.subject_id::text,coalesce(identity.binding_id::text,''),coalesce(binding.ref,''),
 identity.target_ref,identity.revision,identity.credential_policy,setup.material_namespace,
 setup.expected_connection_version,setup.expected_binding_version,setup.expected_binding_generation,
 setup.definition_digest,setup.configuration_digest,setup.content_sha256,setup.content_bytes,setup.ref
FROM control_plane.ssh_identity_setups setup
JOIN control_plane.organizations organization ON organization.id=setup.organization_id
JOIN control_plane.subjects actor ON actor.id=setup.actor_id AND actor.organization_id=setup.organization_id AND actor.kind='USER'
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id AND connection.organization_id=setup.organization_id
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
 AND identity.organization_id=setup.organization_id AND identity.connection_id=setup.connection_id AND identity.purpose='SSH_TARGET'
LEFT JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
 AND binding.organization_id=identity.organization_id AND binding.connection_id=identity.connection_id AND binding.subject_id=identity.subject_id
JOIN control_plane.ssh_identity_upload_attempts attempt ON attempt.setup_id=setup.id AND attempt.organization_id=setup.organization_id
WHERE setup.ref=@setup_ref AND attempt.ref=@attempt_ref AND attempt.generation=@generation
 AND attempt.workload_instance=@instance AND attempt.fence_sha256=@fence_digest
 AND attempt.snapshot_sha256=setup.snapshot_sha256;
