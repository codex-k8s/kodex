-- name: catalog_card_connection_state :exec
UPDATE control_plane.integration_connections SET state=$1,updated_at=clock_timestamp()
WHERE ref='conn_card_projection';
