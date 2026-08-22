-- name: platform__commands_changeagent_update_agents_name_purpose_role_description :one
UPDATE control_plane.agents a
SET name = $4,
    purpose = $5,
    role_description = $6,
    avatar_url = $7,
    runtime_key = COALESCE(NULLIF($8, ''), a.runtime_key),
    role_definition_id = CASE
        WHEN $9 = '' THEN a.role_definition_id
        ELSE (
            SELECT role.id
            FROM control_plane.role_definitions role
            WHERE role.organization_id = a.organization_id
              AND role.project_id = a.project_id
              AND role.ref = $9
              AND role.lifecycle = 'ACTIVE'
        )
    END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE a.organization_id = $1::uuid
  AND a.ref = $2
  AND a.version = $3
  AND a.system_key IS NULL
RETURNING a.project_id::text, a.ref, a.name, a.purpose, a.role_description, a.avatar_url,
          a.state, a.enabled, a.version, a.created_at, a.updated_at
