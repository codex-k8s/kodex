-- +goose Up
SET ROLE control_plane_owner;

-- Существующие agent runtime revisions остаются неизменяемыми: обновляется
-- только server-owned default, из которого создаются новые конфигурации.
UPDATE control_plane.runtime_profiles
SET model = 'gpt-5.4',
    version = version + 1
WHERE stable_key = 'builtin-safe-runtime'
  AND provider = 'openai-codex'
  AND model = 'gpt-5.6-sol'
  AND runtime_revision = 'runtime-v1';

RESET ROLE;
