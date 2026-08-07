-- name: target_runtime_derived_resources__list :many
SELECT jsonb_build_object(
    'id', id,
    'organization_id', organization_id,
    'project_id', project_id,
    'parent_id', parent_id,
    'owner_actor_id', owner_actor_id,
    'kind', kind,
    'state', state,
    'version', version,
    'historical', true,
    'spec', spec
)::text
FROM control_plane.runtime_derived_resources
ORDER BY organization_id, project_id, kind, id, version;
