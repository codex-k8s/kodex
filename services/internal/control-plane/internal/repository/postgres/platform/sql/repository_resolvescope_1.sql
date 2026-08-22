-- name: platform__repository_resolvescope_1 :one
SELECT o.id::text,o.ref,s.id::text,s.ref,s.display_name,
		COALESCE((SELECT m.role FROM control_plane.memberships m WHERE m.organization_id=o.id AND m.subject_id=s.id AND m.project_id IS NULL AND m.active LIMIT 1),'MEMBER')
		FROM control_plane.organizations o
		JOIN control_plane.subjects s ON s.organization_id=o.id AND s.ref=$1
		WHERE o.ref=$2 AND EXISTS (SELECT 1 FROM control_plane.memberships m WHERE m.organization_id=o.id AND m.subject_id=s.id AND m.active)
