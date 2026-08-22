-- name: platform__commands_changeinstructions_4 :one
SELECT ref,content FROM control_plane.instruction_versions WHERE agent_id=$1::uuid AND state IN ('DRAFT','INVALID','VALID') FOR UPDATE
