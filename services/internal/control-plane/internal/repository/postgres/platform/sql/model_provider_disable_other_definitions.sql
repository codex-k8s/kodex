-- name: model_provider_disable_other_definitions :exec
UPDATE control_plane.provider_definitions
SET enabled = false
WHERE (stable_key = 'openai-codex' OR (
    capabilities->>'runtimeEngine' = 'opencode'
    AND capabilities->>'managedBy' = 'git'
    AND NOT (stable_key = ANY(@definition_keys::text[]))))
  AND enabled;
