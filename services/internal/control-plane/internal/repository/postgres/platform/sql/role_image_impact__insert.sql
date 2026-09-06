-- name: role_image_impact__insert :one
INSERT INTO control_plane.role_image_impact_plans
 (ref,organization_id,actor_id,configuration_id,revision_id,artifact_id,snapshot,digest)
VALUES (@ref,@organization_id::uuid,@actor_id::uuid,@configuration_id::uuid,@revision_id::uuid,
 @artifact_id::uuid,@snapshot::jsonb,@digest)
RETURNING id::text,created_at,expires_at;
