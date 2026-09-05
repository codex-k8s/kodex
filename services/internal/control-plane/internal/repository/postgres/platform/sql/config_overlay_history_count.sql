-- name: config_overlay_history_count :one
SELECT count(*)
FROM control_plane.agent_config_overlay_versions v
JOIN control_plane.agents a ON a.id=v.agent_id AND a.organization_id=v.organization_id
WHERE v.organization_id=$1::uuid AND a.ref=$2
  AND v.state IN ('PUBLISHED','SUPERSEDED') AND v.published_at IS NOT NULL
  AND ($3='' OR strpos(lower(v.ref),lower($3))>0);
