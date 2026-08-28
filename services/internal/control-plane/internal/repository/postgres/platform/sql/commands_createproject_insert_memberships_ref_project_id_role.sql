-- name: commands_createproject_insert_memberships_ref_project_id_role :exec
SELECT *
FROM control_plane.create_project_membership(
    $1,
    $2::uuid,
    (SELECT id FROM control_plane.projects WHERE organization_id = $2::uuid AND ref = $3),
    $4::uuid,
    $4::uuid,
    $5::text[]
);
