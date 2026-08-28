-- name: access_list_subjects :many
SELECT s.ref, s.kind, s.display_name, s.active,
       COALESCE(array_agg(g.ref ORDER BY g.ref) FILTER (WHERE g.ref IS NOT NULL), '{}')::text[]
FROM control_plane.subjects s
LEFT JOIN control_plane.oidc_group_memberships gm
  ON gm.organization_id = s.organization_id AND gm.subject_id = s.id
LEFT JOIN control_plane.oidc_groups g ON g.id = gm.group_id
WHERE s.organization_id = @organization_id::uuid
  AND (@kind = '' OR s.kind = @kind)
  AND (@query = '' OR lower(s.display_name) LIKE '%' || lower(@query) || '%')
  AND (@cursor = '' OR s.ref > @cursor)
GROUP BY s.id, s.ref, s.kind, s.display_name, s.active
ORDER BY s.ref
LIMIT @limit
