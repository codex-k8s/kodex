-- name: target_protected_history__list :many
SELECT jsonb_build_object(
    'id', resource_id,
    'organization_id', organization_id,
    'project_id', project_id,
    'owner_actor_id', owner_actor_id,
    'kind', resource_kind,
    'state', snapshot ->> 'state',
    'version', resource_version,
    'projection_sha256', snapshot_sha256,
    'historical', true,
    'spec', snapshot -> 'spec'
)::text
FROM control_plane.protected_resource_history
ORDER BY organization_id, project_id, resource_kind, resource_id, resource_version;
