-- name: commands_changeinstructions_update_agents_version_updated_at :exec
UPDATE control_plane.agents SET version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
