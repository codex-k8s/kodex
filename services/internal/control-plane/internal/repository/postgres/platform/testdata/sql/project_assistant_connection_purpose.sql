-- name: project_assistant_connection_purpose :one
SELECT purpose.project_ref,purpose.profile_ref,purpose.assistant_ref,purpose.profile_version,purpose.agent_version,
       organization.ref,connection.organization_id=purpose.organization_id
FROM control_plane.project_assistant_connection_purposes purpose
JOIN control_plane.integration_connections connection ON connection.id=purpose.connection_id
JOIN control_plane.organizations organization ON organization.id=purpose.organization_id
WHERE connection.ref=$1;
