-- name: platform__repository_bootstrap_2 :one
INSERT INTO control_plane.organizations (ref, name)
		VALUES ($1, 'MatterCodex') RETURNING id::text
