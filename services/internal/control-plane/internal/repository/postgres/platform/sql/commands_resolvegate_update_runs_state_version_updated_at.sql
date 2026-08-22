-- name: commands_resolvegate_update_runs_state_version_updated_at :exec
UPDATE control_plane.runs SET state=$2,version=version+1,updated_at=clock_timestamp(),finished_at=CASE WHEN $2 IN ('FAILED','CANCELLED') THEN clock_timestamp() ELSE NULL END WHERE id=$1::uuid
