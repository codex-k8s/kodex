-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Эти таблицы созданы после ALTER DEFAULT PRIVILEGES с DML для runtime.
-- Отзыв PUBLIC не отменяет отдельный grant роли. Только SECURITY DEFINER
-- purge-владелец создаёт и очищает разрешение на удаление точных строк.
-- SELECT нужен существующим SECURITY INVOKER guards; он не создаёт authority.
REVOKE INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER
    ON control_plane.project_purge_context,control_plane.project_purge_targets FROM control_plane_runtime;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'project purge authority migration is forward-only';
END $$;
-- +goose StatementEnd
