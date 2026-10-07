-- name: integration_egress_origins :many
WITH origins AS (
SELECT connection.organization_id,connection.ref,connection.definition_key,connection.definition_version,connection.definition_digest,
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
  AND connection.definition_key IN ('openapi-mcp','context7')
UNION ALL
-- SHIPPED Context7 исполняется без managed binding, но только с текущими
-- точными pins владельца; Go сверяет их с тем же immutable shipped registry.
SELECT connection.organization_id,connection.ref,connection.definition_key,connection.definition_version,connection.definition_digest,
       connection.public_configuration,''::text,''::text
FROM control_plane.integration_connections connection
JOIN control_plane.integration_definitions definition
  ON definition.stable_key=connection.definition_key AND definition.enabled
 AND definition.definition_version=connection.definition_version AND definition.digest=connection.definition_digest
WHERE connection.lifecycle_state='ACTIVE' AND connection.enabled
  AND connection.definition_key='context7'
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.managed_configuration_bindings binding
    WHERE binding.organization_id=connection.organization_id
      AND binding.consumer_kind='INTEGRATION_CONNECTION' AND binding.consumer_ref=connection.ref
      AND binding.configuration_kind='INTEGRATION_DEFINITION'
  )
)
SELECT definition_key,definition_version,definition_digest,
       public_configuration,content_format,content
FROM origins
ORDER BY organization_id,ref;
