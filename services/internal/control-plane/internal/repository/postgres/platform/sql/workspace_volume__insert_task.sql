-- name: workspace_volume__insert_task :one
INSERT INTO control_plane.session_workspace_volume_tasks(ref,volume_id,volume_generation,origin_node_id,
 root_run_id,origin_actor_id,kind,frozen_input)
VALUES(@task_ref,@volume_id::uuid,@volume_generation,@node_id::uuid,@root_run_id::uuid,@actor_id::uuid,@kind,@frozen_input::jsonb)
RETURNING ref;
