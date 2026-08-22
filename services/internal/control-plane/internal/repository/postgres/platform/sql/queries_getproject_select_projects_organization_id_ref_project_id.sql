-- name: platform__queries_getproject_select_projects_organization_id_ref_project_id :one
SELECT p.id::text,p.ref,p.name,p.purpose,p.language,p.lifecycle,p.version,p.created_at,p.updated_at
		FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.ref=$2
		AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
