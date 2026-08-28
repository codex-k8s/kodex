-- name: repository_disable_unshipped_integration_definitions :exec
UPDATE control_plane.integration_definitions
SET enabled=false,version=version+1
WHERE NOT (stable_key=ANY($1::text[])) AND enabled
