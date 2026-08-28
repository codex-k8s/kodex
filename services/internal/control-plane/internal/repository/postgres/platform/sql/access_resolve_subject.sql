-- name: access_resolve_subject :one
SELECT s.id::text, s.ref, s.kind, s.display_name, s.active,
       COALESCE(array_agg(g.ref ORDER BY g.ref) FILTER (WHERE g.ref IS NOT NULL), '{}')::text[]
FROM control_plane.subjects s
LEFT JOIN control_plane.oidc_group_memberships gm
  ON gm.organization_id = s.organization_id AND gm.subject_id = s.id
LEFT JOIN control_plane.oidc_groups g ON g.id = gm.group_id
WHERE s.organization_id = @organization_id::uuid AND s.ref = @subject_ref
GROUP BY s.id, s.ref, s.kind, s.display_name, s.active
