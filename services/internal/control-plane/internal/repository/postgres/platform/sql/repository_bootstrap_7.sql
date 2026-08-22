-- name: platform__repository_bootstrap_7 :exec
INSERT INTO control_plane.integration_definitions
			(stable_key,name,description,category,capabilities,configuration_schema)
			VALUES ($1,$2,$3,$4,$5,'{"type":"object","additionalProperties":false}'::jsonb)
