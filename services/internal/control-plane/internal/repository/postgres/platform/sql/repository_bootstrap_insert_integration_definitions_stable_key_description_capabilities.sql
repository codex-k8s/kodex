-- name: platform__repository_bootstrap_insert_integration_definitions_stable_key_description_capabilities :exec
INSERT INTO control_plane.integration_definitions
			(stable_key,name,description,category,capabilities,configuration_schema)
			VALUES ($1,$2,$3,$4,$5,$6)
