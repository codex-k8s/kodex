-- name: integration_egress_origins :many
SELECT connection.definition_key,connection.definition_version,connection.definition_digest,
       connection.public_configuration,revision.content_format,revision.content
FROM control_plane.integration_connections connection
JOIN control_plane.managed_configuration_bindings binding
  ON binding.organization_id=connection.organization_id
 AND binding.consumer_kind='INTEGRATION_CONNECTION' AND binding.consumer_ref=connection.ref
 AND binding.configuration_kind='INTEGRATION_DEFINITION'
JOIN control_plane.managed_configuration_sets configuration
  ON configuration.id=binding.configuration_set_id AND configuration.organization_id=binding.organization_id
 AND configuration.kind='INTEGRATION_DEFINITION'
JOIN control_plane.managed_configuration_revisions revision
  ON revision.id=binding.configuration_revision_id AND revision.configuration_set_id=configuration.id
 AND revision.organization_id=connection.organization_id AND revision.state='PUBLISHED'
 AND configuration.current_revision_id=revision.id
WHERE connection.lifecycle_state='ACTIVE' AND connection.enabled
  AND connection.definition_key='openapi-mcp'
ORDER BY connection.organization_id,connection.ref;
