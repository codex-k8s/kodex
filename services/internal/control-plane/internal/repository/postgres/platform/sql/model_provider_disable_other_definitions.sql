-- name: model_provider_disable_other_definitions :exec
UPDATE control_plane.provider_definitions
SET enabled = false
WHERE (stable_key = 'openai-codex' OR (
    capabilities->>'runtimeEngine' = 'opencode'
    AND capabilities->>'managedBy' = 'git'
    AND stable_key <> @definition_key))
  AND enabled;
