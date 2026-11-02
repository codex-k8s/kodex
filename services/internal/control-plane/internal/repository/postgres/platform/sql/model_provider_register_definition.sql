-- name: model_provider_register_definition :one
INSERT INTO control_plane.provider_definitions AS current
    (stable_key, name, adapter_key, capabilities, enabled)
VALUES (@definition_key, 'OpenCode (managed MCP)', @definition_key, @capabilities::jsonb, true)
ON CONFLICT (stable_key) DO UPDATE
SET capabilities = EXCLUDED.capabilities, enabled = true
WHERE current.adapter_key = @definition_key
  AND current.capabilities->>'managedBy' = 'git'
  AND current.capabilities->'modelProvider'->>'base_url' = @base_url
RETURNING stable_key;
