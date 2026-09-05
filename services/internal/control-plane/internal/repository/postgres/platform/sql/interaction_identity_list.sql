-- name: interaction_identity_list :many
SELECT mapping.ref, mapping.version, connection.ref, mapping.connection_version,
       mapping.external_team_ref, mapping.external_channel_ref, mapping.external_user_digest, subject.ref, mapping.state
FROM control_plane.interaction_identities mapping
JOIN control_plane.integration_connections connection ON connection.id=mapping.connection_id
JOIN control_plane.subjects subject ON subject.id=mapping.subject_id
WHERE mapping.organization_id=@organization_id::uuid
  AND (@connection_ref='' OR connection.ref=@connection_ref)
  AND mapping.ref > @cursor_ref
  AND control_plane.catalog_resource_visible(mapping.organization_id,@actor_id::uuid,'integration.manage','INTEGRATION',
      connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp())
ORDER BY mapping.ref LIMIT @page_size;
