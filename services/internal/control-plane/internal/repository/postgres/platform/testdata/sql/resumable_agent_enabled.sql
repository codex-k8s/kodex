-- name: resumable_agent_enabled :exec
UPDATE control_plane.agents SET enabled = $2 WHERE ref = $1;
