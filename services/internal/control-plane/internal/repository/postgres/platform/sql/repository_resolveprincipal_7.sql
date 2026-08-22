-- name: platform__repository_resolveprincipal_7 :one
SELECT COALESCE(bool_or(active),false) FROM control_plane.memberships
			WHERE organization_id=$1::uuid AND subject_id=$2::uuid
