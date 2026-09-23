-- +goose Up
SET ROLE control_plane_owner;

-- Меняется только серверный профиль для новых сотрудников. Уже созданные
-- immutable runtime-конфигурации и явный выбор владельца не переписываются.
UPDATE control_plane.runtime_profiles
SET model = 'gpt-6-sol',
    version = version + 1
WHERE stable_key = 'builtin-safe-runtime'
  AND provider = 'openai-codex'
  AND model = 'gpt-5.4'
  AND runtime_revision = 'runtime-v1';

RESET ROLE;
