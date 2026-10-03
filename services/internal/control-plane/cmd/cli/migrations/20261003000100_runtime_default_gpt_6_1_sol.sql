-- +goose Up
SET ROLE control_plane_owner;

-- Меняем только прежний server-owned default. Явно выбранные модели,
-- immutable конфигурации сотрудников и снимки текущих запусков сохраняются.
UPDATE control_plane.runtime_profiles
SET model = 'gpt-6.1-sol',
    version = version + 1
WHERE stable_key = 'builtin-safe-runtime'
  AND provider = 'openai-codex'
  AND model = 'gpt-5.6-sol'
  AND runtime_revision = 'runtime-v1';

RESET ROLE;
