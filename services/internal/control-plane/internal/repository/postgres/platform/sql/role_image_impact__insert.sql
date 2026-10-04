-- name: role_image_impact__insert :one
INSERT INTO control_plane.role_image_impact_plans
 (ref,organization_id,actor_id,configuration_id,revision_id,artifact_id,snapshot,digest,owner_snapshot_revision)
VALUES (@ref,@organization_id::uuid,@actor_id::uuid,@configuration_id::uuid,@revision_id::uuid,
 @artifact_id::uuid,@snapshot::jsonb,@digest,2)
RETURNING id::text,created_at,expires_at;
