-- name: project_membership__update :one
SELECT updated.binding_ref,
       @permissions::text[],
       updated.binding_state = 'ACTIVE',
       updated.binding_version
FROM control_plane.update_project_membership(
    @membership_id::uuid,
    @organization_id::uuid,
    @project_id::uuid,
    @expected_version,
    @permissions::text[],
    @active,
    @actor_id::uuid
) updated;
