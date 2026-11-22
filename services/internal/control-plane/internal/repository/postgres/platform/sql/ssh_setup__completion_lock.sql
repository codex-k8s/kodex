-- name: ssh_setup__completion_lock :one
WITH connection AS MATERIALIZED (
 SELECT id FROM control_plane.integration_connections WHERE id=@connection_id::uuid AND organization_id=@organization_id::uuid FOR UPDATE
), binding AS MATERIALIZED (
 SELECT b.id FROM control_plane.integration_personal_credentials b CROSS JOIN connection
 WHERE b.id=nullif(@binding_id,'')::uuid AND b.organization_id=@organization_id::uuid AND b.connection_id=connection.id FOR UPDATE OF b
), setup AS MATERIALIZED (
 SELECT s.id FROM control_plane.ssh_identity_setups s CROSS JOIN connection
 WHERE s.id=@setup_id::uuid AND s.connection_id=connection.id AND (SELECT count(*) FROM binding)>=0 FOR UPDATE OF s
)
SELECT a.id::text,a.mode,a.state,a.expires_at,clock_timestamp()
FROM control_plane.ssh_identity_upload_attempts a CROSS JOIN setup
WHERE a.setup_id=setup.id AND a.ref=@attempt_ref AND a.generation=@generation
 AND a.workload_instance=@instance AND a.fence_sha256=@fence_digest FOR UPDATE OF a;
