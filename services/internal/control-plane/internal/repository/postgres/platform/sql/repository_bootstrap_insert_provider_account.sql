-- name: repository_bootstrap_insert_provider_account :one
INSERT INTO control_plane.provider_accounts
    (ref, organization_id, definition_key, stable_key, name, state, enabled, created_by)
VALUES
    ($1, $2::uuid, 'openai-codex', 'default-openai-codex',
     'i18n:DEFAULT_PROVIDER_ACCOUNT_NAME', 'AUTHORIZED', true, $3::uuid)
RETURNING id::text;
