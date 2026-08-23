-- name: commands_createagent_insert_agents_ref_project_id_purpose :one
INSERT INTO control_plane.agents
    (ref, organization_id, project_id, role_definition_id, name, purpose, role_description, avatar_url, runtime_key, state, enabled, created_by)
VALUES
    ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, 'READY', true, $10::uuid)
RETURNING id::text, ref, name, purpose, role_description, avatar_url, state, enabled, version, created_at, updated_at
