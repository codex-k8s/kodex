-- name: runtime_catalog__read_overlay_metadata :many
SELECT ref, diagnostics, schema_revision, schema_digest
FROM control_plane.agent_config_overlay_versions
WHERE organization_id = $1::uuid AND ref = ANY($2::text[]);
