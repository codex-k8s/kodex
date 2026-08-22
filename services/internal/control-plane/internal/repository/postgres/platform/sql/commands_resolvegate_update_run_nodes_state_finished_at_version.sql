-- name: commands_resolvegate_update_run_nodes_state_finished_at_version :exec
UPDATE control_plane.run_nodes SET state=$2,finished_at=clock_timestamp(),version=version+1,next_actions=ARRAY['OPEN'] WHERE id=$1::uuid
