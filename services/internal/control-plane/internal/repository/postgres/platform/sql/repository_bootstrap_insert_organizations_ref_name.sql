-- name: repository_bootstrap_insert_organizations_ref_name :one
INSERT INTO control_plane.organizations (ref, name)
		VALUES ($1, 'Kodex') RETURNING id::text
