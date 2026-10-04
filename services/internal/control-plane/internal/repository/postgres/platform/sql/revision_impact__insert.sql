-- name: revision_impact__insert :one
INSERT INTO control_plane.revision_impact_plans(ref,organization_id,actor_id,kind,snapshot,digest,owner_snapshot_revision)
VALUES(@ref,@organization_id::uuid,@actor_id::uuid,@kind,@snapshot::jsonb,@digest,2)
RETURNING id::text,created_at,expires_at;
