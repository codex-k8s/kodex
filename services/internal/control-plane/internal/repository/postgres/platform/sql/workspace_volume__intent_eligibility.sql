-- name: workspace_volume__intent_eligibility :one
SELECT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks t
 WHERE t.volume_id=@volume_id::uuid AND t.volume_generation=@generation AND t.state IN('READY','CLAIMED')),
 EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks t
 WHERE t.volume_id=@volume_id::uuid AND t.volume_generation=@generation AND t.kind='PREPARE_WORKSPACE_VOLUME'
  AND t.origin_node_id=@node_id::uuid AND t.root_run_id=@root_run_id::uuid AND t.origin_actor_id=@actor_id::uuid
  AND (t.frozen_input->>'read_only')::boolean=@read_only::boolean);
