-- +goose Up
SET ROLE control_plane_owner;

-- Поколение описывает содержимое, а не единственную публикацию объекта.
-- Повторный snapshot после restore/GC получает отдельный immutable receipt.
ALTER TABLE control_plane.session_archives
    DROP CONSTRAINT session_archives_session_id_content_generation_key,
    ADD CONSTRAINT session_archives_publication_key
        UNIQUE (session_id, content_generation, object_key);

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'session archive publication identity is forward-only';
END $$;
-- +goose StatementEnd
