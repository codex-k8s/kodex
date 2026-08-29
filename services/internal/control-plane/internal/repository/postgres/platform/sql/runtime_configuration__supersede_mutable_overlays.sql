-- name: runtime_configuration__supersede_mutable_overlays :execrows
UPDATE control_plane.agent_config_overlay_versions
SET state = 'SUPERSEDED'
WHERE agent_id = $1::uuid
  AND state IN ('DRAFT', 'VALID', 'INVALID');
