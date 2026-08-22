-- name: platform__repository_resolveprincipal_2 :one
SELECT id::text,ref FROM control_plane.subjects
		WHERE organization_id=$1::uuid AND external_subject_digest=$2
