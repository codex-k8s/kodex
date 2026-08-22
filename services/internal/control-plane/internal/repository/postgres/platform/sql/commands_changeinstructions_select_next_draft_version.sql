-- name: commands_changeinstructions_select_next_draft_version :one
SELECT COALESCE(max(version_number),0)+1 FROM control_plane.instruction_versions WHERE agent_id=$1::uuid
