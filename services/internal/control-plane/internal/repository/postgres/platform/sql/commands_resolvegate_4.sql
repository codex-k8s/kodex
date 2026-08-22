-- name: platform__commands_resolvegate_4 :exec
UPDATE control_plane.runs SET state=$2,version=version+1,updated_at=clock_timestamp(),finished_at=CASE WHEN $2='FAILED' THEN clock_timestamp() ELSE NULL END WHERE id=$1::uuid
