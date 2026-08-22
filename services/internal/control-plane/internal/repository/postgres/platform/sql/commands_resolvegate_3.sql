-- name: platform__commands_resolvegate_3 :exec
UPDATE control_plane.run_nodes SET state=$2,finished_at=clock_timestamp(),version=version+1,next_actions=ARRAY['OPEN'] WHERE id=$1::uuid
