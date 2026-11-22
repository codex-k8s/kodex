-- name: email_credential_owner__read :one
SELECT connection.id::text,connection.version,connection.state,connection.enabled,connection.credential_policy,
       COALESCE(connection.mailbox_owner_subject_id::text,''),
       COALESCE(subject.ref,''),COALESCE(subject.kind,''),
       COALESCE(subject.active,false) AND EXISTS(SELECT 1 FROM control_plane.subjects configurer
         WHERE configurer.id=connection.mailbox_owner_configured_by
           AND configurer.organization_id=connection.organization_id AND configurer.kind='USER' AND configurer.active),
       COALESCE(connection.mailbox_owner_configured_by::text,''),
       connection.mailbox_owner_generation,connection.mailbox_owner_state,
       actor.id::text,actor.kind,actor.active
FROM control_plane.integration_connections connection
JOIN control_plane.subjects actor ON actor.organization_id=connection.organization_id
  AND actor.ref=@actor_ref
LEFT JOIN control_plane.subjects subject ON subject.organization_id=connection.organization_id
  AND subject.id=connection.mailbox_owner_subject_id
WHERE connection.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
  AND connection.definition_key='email' AND connection.lifecycle_state='ACTIVE'
FOR UPDATE OF connection;
