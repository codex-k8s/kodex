-- name: platform__commands_changeinstructions_7 :one
SELECT content FROM control_plane.instruction_versions WHERE agent_id=$1::uuid AND ref=$2 AND state='PUBLISHED'
