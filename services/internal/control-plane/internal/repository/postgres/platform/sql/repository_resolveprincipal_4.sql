-- name: platform__repository_resolveprincipal_4 :exec
INSERT INTO control_plane.memberships
			(ref,organization_id,subject_id,role,permissions) VALUES ($1,$2::uuid,$3::uuid,'OWNER',$4)
