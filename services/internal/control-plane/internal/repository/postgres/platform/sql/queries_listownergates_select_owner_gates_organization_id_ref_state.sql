-- name: queries_listownergates_select_owner_gates_organization_id_ref_state :many
WITH visible AS MATERIALIZED (
    SELECT g.ref,g.created_at
    FROM control_plane.owner_gates g
    JOIN control_plane.projects p ON p.id=g.project_id
    WHERE g.organization_id=$1::uuid
      AND ($2='' OR p.ref=$2)
      AND (cardinality($3::text[])=0 OR g.state=ANY($3::text[]))
      AND ($9='' OR strpos(lower(g.title),lower($9))>0 OR strpos(lower(g.prompt),lower($9))>0 OR strpos(lower(g.context_summary),lower($9))>0)
      AND ($4 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
        SELECT 1 FROM control_plane.memberships m
        WHERE m.project_id=g.project_id AND m.subject_id=$5::uuid AND m.active AND 'VIEW'=ANY(m.permissions)
      ))
), page AS (
    SELECT ref,created_at FROM visible
    WHERE $7='' OR (created_at,ref)<($8::timestamptz,$7)
    ORDER BY created_at DESC,ref DESC LIMIT $6
)
SELECT COALESCE(page.ref,''),totals.total FROM (SELECT count(*) AS total FROM visible) totals
LEFT JOIN page ON true ORDER BY page.created_at DESC,page.ref DESC;
