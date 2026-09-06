-- name: config_overlay_history_get :one
SELECT v.ref,v.version_number,v.state,v.content,v.digest,v.validation_errors,v.created_at,v.published_at,v.diagnostics,v.schema_revision,v.schema_digest
FROM control_plane.agent_config_overlay_versions v
JOIN control_plane.agents a ON a.id=v.agent_id AND a.organization_id=v.organization_id
WHERE v.organization_id=$1::uuid AND a.ref=$2
  AND v.state IN ('PUBLISHED','SUPERSEDED') AND v.published_at IS NOT NULL
  AND v.ref=$3;
