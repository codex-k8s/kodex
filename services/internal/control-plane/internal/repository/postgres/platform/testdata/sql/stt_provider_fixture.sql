-- name: stt_provider_fixture :one
WITH definition AS (
 INSERT INTO control_plane.provider_definitions (stable_key, name, adapter_key, capabilities, enabled)
 VALUES ('opencode-openai', 'Synthetic speech provider', 'opencode-openai', $3::jsonb, true)
 RETURNING stable_key
), account AS (
 INSERT INTO control_plane.provider_accounts (ref, organization_id, definition_key, stable_key, name, state, enabled, created_by)
 SELECT 'pacc_component_speech', $1::uuid, stable_key, 'component-speech', 'Synthetic speech account', 'AUTHORIZED', true, $2::uuid FROM definition
 RETURNING id, organization_id, ref
), credential AS (
 INSERT INTO control_plane.provider_credential_revisions (ref, organization_id, provider_account_id, revision_number, secret_name, secret_uid, secret_resource_version, content_sha256, observed_at)
 SELECT 'pcr_component_speech', organization_id, id, 1, 'runtime-provider-component-speech', '31000000-0000-4000-8000-000000000001'::uuid, '1', repeat('a',64), clock_timestamp() FROM account
 RETURNING id, provider_account_id
)
SELECT account.ref, credential.id::text FROM account JOIN credential ON credential.provider_account_id = account.id;
