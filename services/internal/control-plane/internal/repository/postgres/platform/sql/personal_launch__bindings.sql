-- name: personal_launch__bindings :many
SELECT binding.ref, binding.organization_id::text, binding.subject_id::text,
       connection.ref, connection.credential_policy, connection.version, binding.version, binding.generation, binding.state,
       COALESCE(revision.ref, ''), COALESCE(revision.revision, 0), COALESCE(revision.secret_ref, ''),
       COALESCE(revision.secret_uid::text, ''), COALESCE(revision.secret_resource_version, ''),
       COALESCE(revision.content_sha256, ''), revision.created_at,
       COALESCE(verified.ref, ''), COALESCE(binding.verified_definition_digest, ''),
       COALESCE(binding.verified_configuration_digest, ''), binding.verified_capabilities,
       binding.denied_capabilities, binding.observed_permission_profiles, binding.expires_at, binding.github_discovery,
       COALESCE(binding.credential_kind,''), COALESCE(binding.broker_purpose,''),
       COALESCE(broker_revision.ref,''), COALESCE(broker_revision.revision,0),
       COALESCE(broker_revision.purpose,''), COALESCE(broker_revision.target_ref,''), broker_revision.created_at,
       COALESCE(broker_verified.ref,'')
FROM control_plane.integration_personal_credentials binding
JOIN control_plane.integration_connections connection
  ON connection.id = binding.connection_id AND connection.organization_id = binding.organization_id
JOIN control_plane.subjects subject
  ON subject.id = binding.subject_id AND subject.organization_id = binding.organization_id
 AND subject.kind = 'USER' AND subject.active
LEFT JOIN control_plane.integration_personal_credential_revisions revision
  ON revision.id = binding.credential_revision_id AND revision.binding_id = binding.id
 AND revision.organization_id = binding.organization_id
LEFT JOIN control_plane.integration_personal_credential_revisions verified
  ON verified.id = binding.verified_credential_revision_id AND verified.binding_id = binding.id
 AND verified.organization_id = binding.organization_id
LEFT JOIN control_plane.integration_broker_identity_revisions broker_revision
 ON broker_revision.id=binding.broker_identity_revision_id AND broker_revision.binding_id=binding.id
 AND broker_revision.organization_id=binding.organization_id AND broker_revision.purpose=binding.broker_purpose
LEFT JOIN control_plane.integration_broker_identity_revisions broker_verified
 ON broker_verified.id=binding.verified_broker_identity_revision_id AND broker_verified.binding_id=binding.id
 AND broker_verified.organization_id=binding.organization_id AND broker_verified.purpose=binding.broker_purpose
WHERE binding.organization_id = @organization_id::uuid
  AND binding.subject_id = @subject_id::uuid
  AND connection.ref = ANY(@connection_refs::text[])
  AND connection.lifecycle_state = 'ACTIVE'
  AND control_plane.catalog_resource_visible(binding.organization_id, binding.subject_id,
      'integration.view', 'INTEGRATION', connection.id, NULL, connection.created_by, '{}'::jsonb, statement_timestamp())

ORDER BY connection.ref
FOR UPDATE OF binding;
