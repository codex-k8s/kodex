-- name: integration_package__bound_revision :one
SELECT revision.content_format, revision.content
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.managed_configuration_sets configuration
  ON configuration.id = binding.configuration_set_id AND configuration.organization_id = binding.organization_id
JOIN control_plane.managed_configuration_revisions revision
  ON revision.id = binding.configuration_revision_id AND revision.configuration_set_id = configuration.id
  AND revision.organization_id = binding.organization_id
WHERE binding.organization_id = $1::uuid AND binding.consumer_kind = 'INTEGRATION_CONNECTION'
  AND binding.consumer_ref = $2 AND binding.configuration_kind = 'INTEGRATION_DEFINITION'
  AND configuration.kind = 'INTEGRATION_DEFINITION' AND revision.state = 'PUBLISHED';
