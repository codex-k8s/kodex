-- name: platform__queries_listmemberships_1 :many
SELECT m.ref,p.ref,s.ref,s.display_name,s.email_masked,s.active,m.role,m.permissions,m.active,m.version
		FROM control_plane.memberships m JOIN control_plane.projects p ON p.id=m.project_id JOIN control_plane.subjects s ON s.id=m.subject_id
		WHERE m.organization_id=$1::uuid AND p.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships own WHERE own.project_id=p.id AND own.subject_id=$4::uuid AND own.active AND 'MANAGE_MEMBERS'=ANY(own.permissions)))
		ORDER BY s.display_name LIMIT $5
