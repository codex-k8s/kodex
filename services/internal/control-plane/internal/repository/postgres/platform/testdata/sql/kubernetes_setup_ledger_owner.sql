SELECT setup.organization_id::text,setup.connection_id::text,connection.ref,identity.subject_id::text,
 coalesce(identity.binding_id::text,''),coalesce(binding.ref,''),identity.target_ref,
 setup.expected_connection_version,setup.expected_binding_version,setup.expected_binding_generation,
 identity.credential_policy,identity.purpose
FROM control_plane.kubernetes_identity_setups setup
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id
 AND connection.organization_id=setup.organization_id
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
 AND identity.organization_id=setup.organization_id
LEFT JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
 AND binding.organization_id=identity.organization_id
WHERE setup.id=@setup_id::uuid;
