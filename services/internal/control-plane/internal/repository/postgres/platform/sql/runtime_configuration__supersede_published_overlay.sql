-- name: runtime_configuration__supersede_published_overlay :exec
UPDATE control_plane.agent_config_overlay_versions
SET state = 'SUPERSEDED'
WHERE agent_id = $1::uuid
  AND state = 'PUBLISHED';
