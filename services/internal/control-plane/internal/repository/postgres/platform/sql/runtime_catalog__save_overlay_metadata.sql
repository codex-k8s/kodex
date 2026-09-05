-- name: runtime_catalog__save_overlay_metadata :exec
UPDATE control_plane.agent_config_overlay_versions
SET diagnostics = $3::jsonb, schema_revision = $4, schema_digest = $5
WHERE organization_id = $1::uuid AND ref = $2
  AND (state IN ('DRAFT', 'VALID', 'INVALID') OR schema_revision = '');
