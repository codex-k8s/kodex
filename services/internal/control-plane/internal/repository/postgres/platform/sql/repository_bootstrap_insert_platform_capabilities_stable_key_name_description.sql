-- name: repository_bootstrap_insert_platform_capabilities_stable_key_name_description :exec
INSERT INTO control_plane.platform_capabilities
			(stable_key, name, description, risk) VALUES ($1,$2,$3,$4)
