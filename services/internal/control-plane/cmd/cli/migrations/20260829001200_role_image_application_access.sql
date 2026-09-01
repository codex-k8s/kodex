-- +goose Up
SET ROLE control_plane_owner;

UPDATE control_plane.permission_registry
SET resource_kinds = ARRAY(
    SELECT DISTINCT resource_kind
    FROM unnest(resource_kinds || ARRAY['ROLE_IMAGE']::text[]) resource_kind
    ORDER BY resource_kind
)
WHERE permission_key = 'project.view';

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

UPDATE control_plane.permission_registry
SET resource_kinds = array_remove(resource_kinds, 'ROLE_IMAGE')
WHERE permission_key = 'project.view';

RESET ROLE;
