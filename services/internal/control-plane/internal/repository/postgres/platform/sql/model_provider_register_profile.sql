-- name: model_provider_register_profile :one
INSERT INTO control_plane.runtime_profiles AS current
    (stable_key, name, provider, model, runtime_revision, resource_limits, enabled)
VALUES (@profile_key, 'OpenCode (managed MCP)', @definition_key, @model, @runtime_revision,
        @resource_limits::jsonb, true)
ON CONFLICT (stable_key) DO UPDATE SET enabled = true
WHERE current.provider = EXCLUDED.provider
  AND current.model = EXCLUDED.model
  AND current.runtime_revision = EXCLUDED.runtime_revision
  AND current.resource_limits = EXCLUDED.resource_limits
RETURNING stable_key;
