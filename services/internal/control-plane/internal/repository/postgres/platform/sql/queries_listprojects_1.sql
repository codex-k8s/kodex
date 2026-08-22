-- name: platform__queries_listprojects_1 :many
SELECT p.ref,p.name,p.purpose,p.language,p.lifecycle,p.version,p.created_at,p.updated_at
		FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.lifecycle<>'ARCHIVED'
		AND ($2 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$3::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
		AND ($4='' OR p.name ILIKE '%'||$4||'%' OR p.purpose ILIKE '%'||$4||'%') ORDER BY p.updated_at DESC LIMIT $5
