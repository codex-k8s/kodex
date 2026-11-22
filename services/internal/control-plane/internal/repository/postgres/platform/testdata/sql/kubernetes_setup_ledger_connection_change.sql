UPDATE control_plane.integration_connections
SET public_configuration=public_configuration||'{"synthetic_revision":"changed"}'::jsonb,version=version+1
WHERE ref='conn_setup_ledger';
