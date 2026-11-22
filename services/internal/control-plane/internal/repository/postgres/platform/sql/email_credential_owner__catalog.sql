-- name: email_credential_owner__catalog :many
SELECT connection.ref,connection.name,connection.version,connection.enabled,
       connection.credential_policy,connection.mailbox_owner_state,connection.mailbox_owner_generation,
       COALESCE(subject.ref,''),
       connection.mailbox_owner_state='ACTIVE' AND COALESCE(subject.active,false) AS owner_eligible,
       connection.mailbox_owner_state='ACTIVE' AND COALESCE(subject.active,false)
         AND control_plane.integration_connection_configuration_ready(connection.id)
         AND connection.state='CONNECTED' AS available
FROM control_plane.integration_connections connection
JOIN control_plane.subjects actor ON actor.id=@subject_id::uuid
  AND actor.organization_id=connection.organization_id AND actor.kind='USER' AND actor.active
LEFT JOIN control_plane.subjects subject ON subject.id=connection.mailbox_owner_subject_id
  AND subject.organization_id=connection.organization_id
WHERE connection.organization_id=@organization_id::uuid AND connection.definition_key='email'
  AND connection.lifecycle_state='ACTIVE'
  AND (connection.mailbox_owner_configured_by=actor.id
    OR (connection.mailbox_owner_state='UNCONFIGURED' AND connection.created_by=actor.id))
  AND connection.ref>@after
  AND (@query='' OR strpos(lower(connection.name||' '||connection.ref),lower(@query))>0)
  AND control_plane.catalog_resource_visible(connection.organization_id,actor.id,
    'integration.manage','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp())
ORDER BY connection.ref
LIMIT @limit;
