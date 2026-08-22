-- name: platform__commands_changeinstructions_6 :exec
UPDATE control_plane.instruction_versions SET state='PUBLISHED',published_at=clock_timestamp() WHERE agent_id=$1::uuid AND state='VALID'
