-- name: platform__configuration_changeconnection_4 :exec
UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1)
