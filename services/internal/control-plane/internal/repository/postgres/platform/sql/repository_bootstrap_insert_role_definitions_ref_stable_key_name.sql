-- name: platform__repository_bootstrap_insert_role_definitions_ref_stable_key_name :one
INSERT INTO control_plane.role_definitions
    (ref, organization_id, stable_key, name, role_type, description, default_policies, created_by)
VALUES
    ($1, $2::uuid, 'system-assistant', 'i18n:SYSTEM_ASSISTANT_NAME', 'SYSTEM_ASSISTANT',
     'i18n:SYSTEM_ASSISTANT_ROLE_DESCRIPTION', '{}'::jsonb, $3::uuid)
RETURNING id::text
