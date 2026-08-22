-- name: platform__commands_changeinstructions_2 :one
SELECT COALESCE(max(version_number),0)+1 FROM control_plane.instruction_versions WHERE agent_id=$1::uuid
