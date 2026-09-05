-- name: config_overlay_history_list :many
SELECT v.ref,v.version_number,v.state,v.content,v.digest,v.validation_errors,v.created_at,v.published_at,v.diagnostics,v.schema_revision,v.schema_digest
FROM control_plane.agent_config_overlay_versions v
JOIN control_plane.agents a ON a.id=v.agent_id AND a.organization_id=v.organization_id
WHERE v.organization_id=$1::uuid AND a.ref=$2
  AND v.state IN ('PUBLISHED','SUPERSEDED') AND v.published_at IS NOT NULL
  AND ($3='' OR strpos(lower(v.ref),lower($3))>0)
  AND ($4::bigint=0 OR (v.version_number,v.ref)<($4::bigint,$5))
ORDER BY v.version_number DESC,v.ref DESC
LIMIT $6;
