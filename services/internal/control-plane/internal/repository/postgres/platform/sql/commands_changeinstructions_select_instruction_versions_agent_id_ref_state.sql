-- name: platform__commands_changeinstructions_select_instruction_versions_agent_id_ref_state :one
SELECT content FROM control_plane.instruction_versions WHERE agent_id=$1::uuid AND ref=$2 AND state='PUBLISHED'
