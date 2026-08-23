-- name: commands_createagent_insert_role_definitions_ref_organization_id_project_id_name :one
INSERT INTO control_plane.role_definitions
    (ref, organization_id, project_id, name, role_type, description, default_policies, created_by)
VALUES
    ($1, $2::uuid, $3::uuid, $4, 'CUSTOM', $5, '{}'::jsonb, $6::uuid)
RETURNING id::text, ref, name
