-- name: repository_bootstrap_insert_provider_definition :exec
INSERT INTO control_plane.provider_definitions
    (stable_key, name, adapter_key, capabilities)
VALUES
    ('openai-codex', 'OpenAI Codex', 'openai-codex',
     '{"sessionAffinity":true,"deviceAuthorization":true,"models":true}'::jsonb);
