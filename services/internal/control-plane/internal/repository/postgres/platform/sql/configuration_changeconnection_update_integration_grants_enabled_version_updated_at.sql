-- name: configuration_changeconnection_update_integration_grants_enabled_version_updated_at :exec
UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1)
