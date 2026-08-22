-- name: platform__commands_changeinstructions_10 :exec
UPDATE control_plane.agents SET version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
