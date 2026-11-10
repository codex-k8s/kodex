-- name: integration_pending_connection_fixture :exec
UPDATE control_plane.integration_connections
SET public_configuration=jsonb_set(public_configuration,'{owner}',to_jsonb(@owner::text)),
    updated_at=clock_timestamp()
WHERE ref=@connection_ref;
