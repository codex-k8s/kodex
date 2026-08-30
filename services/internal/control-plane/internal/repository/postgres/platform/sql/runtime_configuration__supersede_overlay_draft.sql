-- name: runtime_configuration__supersede_overlay_draft :exec
UPDATE control_plane.agent_config_overlay_versions
SET state = 'SUPERSEDED'
WHERE agent_id = $1::uuid
  AND id = $2::uuid
  AND state = 'VALID';
