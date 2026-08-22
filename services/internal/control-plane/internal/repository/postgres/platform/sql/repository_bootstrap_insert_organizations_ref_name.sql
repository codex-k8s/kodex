-- name: platform__repository_bootstrap_insert_organizations_ref_name :one
INSERT INTO control_plane.organizations (ref, name)
		VALUES ($1, 'MatterCodex') RETURNING id::text
