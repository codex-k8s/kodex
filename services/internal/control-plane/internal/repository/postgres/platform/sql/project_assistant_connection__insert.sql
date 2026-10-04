-- name: project_assistant_connection__insert :exec
INSERT INTO control_plane.project_assistant_connection_purposes
    (organization_id,connection_id,project_ref,profile_ref,assistant_ref,profile_version,agent_version,created_by)
SELECT connection.organization_id,connection.id,@project_ref,@profile_ref,@assistant_ref,
       @profile_version::bigint,@agent_version::bigint,@actor_id::uuid
FROM control_plane.integration_connections connection
WHERE connection.organization_id=@organization_id::uuid AND connection.ref=@connection_ref;
