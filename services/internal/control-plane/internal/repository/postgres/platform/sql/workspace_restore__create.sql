-- name: workspace_restore__create :one
INSERT INTO control_plane.session_archive_tasks(ref,organization_id,project_id,session_id,archive_id,kind,state,content_generation,
 input_digest,object_key,object_version,maximum_attempts,workspace_id,workspace_generation,workspace_input)
VALUES(@task_ref,@organization_id::uuid,@project_id::uuid,@session_id::uuid,@archive_id::uuid,'RESTORE','READY',@content_generation,
 @input_digest,@object_key,@object_version,5,@workspace_id::uuid,@workspace_generation,@workspace_input::jsonb)
RETURNING id::text;
