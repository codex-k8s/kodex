-- name: revision_impact__get :one
SELECT id::text,kind,snapshot,digest,version,state,created_at,expires_at,published_revision_ref
FROM control_plane.revision_impact_plans
WHERE organization_id=@organization_id::uuid AND actor_id=@actor_id::uuid AND ref=@ref
FOR UPDATE;
