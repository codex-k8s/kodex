-- name: configuration_changeintegrationgrant_update_integration_connections_version :exec
UPDATE control_plane.integration_connections SET version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND version=$2
