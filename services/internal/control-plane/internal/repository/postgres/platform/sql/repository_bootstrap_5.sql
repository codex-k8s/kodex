-- name: platform__repository_bootstrap_5 :exec
INSERT INTO control_plane.platform_capabilities
			(stable_key, name, description, risk) VALUES ($1,$2,$3,$4)
