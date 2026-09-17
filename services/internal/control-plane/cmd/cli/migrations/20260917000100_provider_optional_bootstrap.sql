-- +goose Up
SET ROLE control_plane_owner;

-- До авторизации аккаунта существует конфигурация, но не исполняемая сессия.
ALTER TABLE control_plane.assistant_runtime
    ALTER COLUMN system_session_ref DROP NOT NULL;
ALTER TABLE control_plane.assistant_runtime
    ADD CONSTRAINT assistant_runtime_ready_requires_session
    CHECK (system_session_ref IS NOT NULL OR runtime_state NOT IN ('READY', 'BUSY'));

-- Пустой начальный пул не даёт полномочий на выполнение. Публичная команда
-- публикации policy по-прежнему требует хотя бы одного проверенного кандидата.
ALTER TABLE control_plane.provider_account_policy_versions
    DROP CONSTRAINT provider_account_policy_versions_account_candidates_check;
ALTER TABLE control_plane.provider_account_policy_versions
    ADD CONSTRAINT provider_account_policy_versions_account_candidates_check
    CHECK (jsonb_typeof(account_candidates) = 'array' AND
        (jsonb_array_length(account_candidates) BETWEEN 1 AND 128 OR
         (account_candidates = '[]'::jsonb AND mode = 'LEAST_USED' AND version_number = 1)));

RESET ROLE;
