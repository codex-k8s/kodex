-- name: platform__queries_listprojects_select_projects_organization_id_project_id_subject_id :many
SELECT p.id,
       p.ref,
       p.name,
       p.purpose,
       p.language,
       p.lifecycle,
       p.version,
       p.created_at,
       p.updated_at,
       COALESCE((
           SELECT array_agg(DISTINCT permission ORDER BY permission)
           FROM control_plane.memberships membership
           CROSS JOIN LATERAL unnest(membership.permissions) permission
           WHERE membership.organization_id=p.organization_id
             AND membership.project_id=p.id
             AND membership.subject_id=$3::uuid
             AND membership.active
       ), '{}'::text[])
FROM control_plane.projects p
WHERE p.organization_id=$1::uuid
  AND p.lifecycle<>'ARCHIVED'
  AND ($2 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.project_id=p.id
        AND membership.subject_id=$3::uuid
        AND membership.active
        AND 'VIEW'=ANY(membership.permissions)
  ))
  AND ($4='' OR p.name ILIKE '%'||$4||'%' OR p.purpose ILIKE '%'||$4||'%')
ORDER BY p.updated_at DESC
LIMIT $5
