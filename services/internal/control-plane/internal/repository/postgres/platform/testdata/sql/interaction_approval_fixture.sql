-- name: interaction_approval_fixture :one
WITH created AS (
    INSERT INTO control_plane.runs
        (ref,organization_id,project_id,session_id,target_type,target_ref,source,title,task,input,state,initiated_by,result_summary,finished_at)
    SELECT @run_ref,organization_id,project_id,session_id,target_type,target_ref,'CONTROL_CENTER',
        'Synthetic delivery approval','Bounded fixture','{}','SUCCEEDED',initiated_by,'Synthetic bounded result',clock_timestamp()
    FROM control_plane.runs WHERE ref=@original_run_ref
    RETURNING id,organization_id,project_id,target_type,target_ref,initiated_by
), node AS (
    INSERT INTO control_plane.run_nodes
        (ref,organization_id,root_run_id,run_id,type,state,display_name,role,next_actions,finished_at)
    SELECT @node_ref,organization_id,id,id,'ROOT_PROCESS','SUCCEEDED','Synthetic root','Synthetic root',ARRAY['OPEN'],clock_timestamp()
    FROM created RETURNING id
), grant_row AS (
    INSERT INTO control_plane.integration_grants
        (ref,organization_id,connection_id,capability_key,target_kind,target_ref,created_by,risk,resource_kind,definition_version,definition_digest)
    SELECT @grant_ref,created.organization_id,connection.id,'mattermost.notifications',created.target_type,created.target_ref,
        created.initiated_by,'WRITE','MATTERMOST_CHANNEL',connection.definition_version,connection.definition_digest
    FROM created,control_plane.integration_connections connection
    WHERE connection.ref=@connection_ref
    ON CONFLICT DO NOTHING
)
SELECT created.id::text,created.project_id::text FROM created,node;
