-- name: kubernetes_setup__begin_context :one
SELECT public_configuration,clock_timestamp()
FROM control_plane.integration_connections
WHERE organization_id=@organization_id::uuid AND id=@connection_id::uuid AND lifecycle_state='ACTIVE';
