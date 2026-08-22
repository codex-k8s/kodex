-- name: platform__commands_changeinstructions_select_next_rollback_version :one
SELECT max(version_number)+1 FROM control_plane.instruction_versions WHERE agent_id=$1::uuid
