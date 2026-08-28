-- name: access_list_oidc_groups :many
SELECT g.ref, g.display_name,
       CASE WHEN g.last_seen_at < clock_timestamp() - interval '24 hours' THEN 'STALE' ELSE g.state END,
       count(DISTINCT gm.subject_id)::integer,
       count(DISTINCT b.id) FILTER (WHERE b.state = 'ACTIVE')::integer,
       g.last_seen_at, g.synchronized_at
FROM control_plane.oidc_groups g
LEFT JOIN control_plane.oidc_group_memberships gm ON gm.group_id = g.id
LEFT JOIN control_plane.access_bindings b ON b.oidc_group_id = g.id
WHERE g.organization_id = @organization_id::uuid
  AND (@query = '' OR lower(g.display_name) LIKE '%' || lower(@query) || '%')
  AND (@cursor = '' OR g.ref > @cursor)
GROUP BY g.id
ORDER BY g.ref
LIMIT @limit
