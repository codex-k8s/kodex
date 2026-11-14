-- name: personal_credential__read_owned :one
SELECT binding.ref, binding.organization_id::text, binding.subject_id::text,
       connection.ref, connection.credential_policy, connection.version, binding.version, binding.generation, binding.state,
       COALESCE(revision.ref, ''), COALESCE(revision.revision, 0), COALESCE(revision.secret_ref, ''),
       COALESCE(revision.secret_uid::text, ''), COALESCE(revision.secret_resource_version, ''),
       COALESCE(revision.content_sha256, ''), revision.created_at,
       COALESCE(verified.ref, ''), COALESCE(binding.verified_definition_digest, ''),
       COALESCE(binding.verified_configuration_digest, ''), binding.verified_capabilities,
       binding.denied_capabilities, binding.observed_permission_profiles, binding.expires_at, binding.github_discovery
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
WHERE binding.organization_id = @organization_id::uuid
  AND binding.subject_id = @subject_id::uuid
  AND connection.ref = @connection_ref
  AND connection.lifecycle_state = 'ACTIVE'
  AND control_plane.catalog_resource_visible(binding.organization_id, binding.subject_id,
      'integration.view', 'INTEGRATION', connection.id, NULL, connection.created_by, '{}'::jsonb, statement_timestamp())
