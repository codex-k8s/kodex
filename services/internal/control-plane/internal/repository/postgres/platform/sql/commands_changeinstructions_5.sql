-- name: platform__commands_changeinstructions_5 :exec
UPDATE control_plane.instruction_versions SET state=$2,validation_problems=$3 WHERE ref=$1
