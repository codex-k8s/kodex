-- name: interaction_identity_resolve :one
SELECT subject.id::text,subject.ref,subject.display_name,membership.role,mapping.id::text
FROM control_plane.interaction_identities mapping
JOIN control_plane.integration_connections connection ON connection.id=mapping.connection_id
 AND connection.organization_id=mapping.organization_id AND connection.version=mapping.connection_version
JOIN control_plane.subjects subject ON subject.id=mapping.subject_id AND subject.organization_id=mapping.organization_id
JOIN control_plane.memberships membership ON membership.subject_id=subject.id AND membership.organization_id=subject.organization_id
 AND membership.project_id IS NULL AND membership.active
WHERE mapping.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
  AND connection.definition_key='mattermost' AND connection.lifecycle_state='ACTIVE'
  AND connection.enabled AND connection.state IN ('CONNECTED','DEGRADED')
  AND mapping.state='ACTIVE' AND subject.active AND subject.kind='USER'
  AND mapping.external_team_ref=@team_ref AND mapping.external_channel_ref=@channel_ref AND mapping.external_user_digest=@user_digest
FOR SHARE OF mapping,connection,subject,membership;
