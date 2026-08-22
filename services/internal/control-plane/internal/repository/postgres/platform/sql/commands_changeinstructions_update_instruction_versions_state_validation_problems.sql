-- name: platform__commands_changeinstructions_update_instruction_versions_state_validation_problems :exec
UPDATE control_plane.instruction_versions SET state=$2,validation_problems=$3 WHERE ref=$1
