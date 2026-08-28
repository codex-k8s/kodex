-- name: runtime_configuration__validate_overlay :exec
UPDATE control_plane.agent_config_overlay_versions
SET state = $2,
    validation_errors = $3,
    validated_at = clock_timestamp()
WHERE id = $1::uuid
  AND state IN ('DRAFT', 'VALID', 'INVALID');
