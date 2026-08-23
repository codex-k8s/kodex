-- name: bootstrap_component_replace_session_provider_account :exec
WITH candidate AS (
    INSERT INTO control_plane.provider_accounts
        (ref, organization_id, definition_key, stable_key, name, state, enabled, created_by)
    SELECT 'pacc_affinity_test', organization.id, 'openai-codex',
           'affinity-test', 'Affinity test', 'PENDING_AUTHORIZATION', false, subject.id
    FROM control_plane.organizations organization
    JOIN control_plane.subjects subject ON subject.organization_id = organization.id
    WHERE subject.issuer = 'mattercodex-system'
    RETURNING id
)
UPDATE control_plane.sessions
SET provider_account_id = candidate.id
FROM candidate
WHERE sessions.target_type = 'SYSTEM_ASSISTANT';
