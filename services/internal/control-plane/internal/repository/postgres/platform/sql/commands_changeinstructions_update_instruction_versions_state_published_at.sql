-- name: commands_changeinstructions_update_instruction_versions_state_published_at :exec
UPDATE control_plane.instruction_versions SET state='PUBLISHED',published_at=clock_timestamp() WHERE agent_id=$1::uuid AND state='VALID'
