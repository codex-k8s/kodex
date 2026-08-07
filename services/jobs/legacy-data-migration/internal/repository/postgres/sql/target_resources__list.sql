-- name: target_resources__list :many
SELECT jsonb_build_object(
    'id', id,
    'organization_id', organization_id,
    'project_id', project_id,
    'parent_id', parent_id,
    'owner_actor_id', owner_actor_id,
    'kind', kind,
    'name', name,
    'state', state,
    'version', version,
    'created_at', created_at,
    'updated_at', updated_at,
    'spec', spec
)::text
FROM control_plane.resources
WHERE state <> 'DELETED'
ORDER BY organization_id, project_id, kind, id;
