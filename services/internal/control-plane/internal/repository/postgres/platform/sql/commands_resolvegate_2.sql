-- name: platform__commands_resolvegate_2 :exec
UPDATE control_plane.owner_gates SET state=$2,decision=$3,decision_comment=$4,resolved_by=$5::uuid,resolved_at=clock_timestamp(),version=version+1 WHERE id=$1::uuid
