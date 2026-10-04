-- name: assistant_configuration_component_membership :one
WITH revoked AS (
 UPDATE control_plane.access_bindings SET state='REVOKED'
 WHERE organization_id=@organization_id::uuid AND subject_id=@actor_id::uuid AND state='ACTIVE'
 RETURNING ref
)
SELECT array_agg(ref) FROM revoked;
