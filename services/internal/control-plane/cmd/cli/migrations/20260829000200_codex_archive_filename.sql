-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.session_archives
    DROP CONSTRAINT session_archives_source_relative_path_check,
    ADD CONSTRAINT session_archives_source_relative_path_check CHECK (
        source_relative_path ~ '^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$'
        AND char_length(source_relative_path) <= 255
        AND position(E'\\' IN source_relative_path) = 0
        AND position('..' IN source_relative_path) = 0
    );

ALTER TABLE control_plane.session_storage
    DROP CONSTRAINT session_storage_source_relative_path_check,
    ADD CONSTRAINT session_storage_source_relative_path_check CHECK (
        source_relative_path ~ '^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$'
        AND char_length(source_relative_path) <= 255
        AND position(E'\\' IN source_relative_path) = 0
        AND position('..' IN source_relative_path) = 0
    );

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.session_archives
    DROP CONSTRAINT session_archives_source_relative_path_check,
    ADD CONSTRAINT session_archives_source_relative_path_check CHECK (
        source_relative_path ~ '^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[0-9a-f-]{36}\.jsonl$'
        AND source_relative_path !~ '(^|/)\.\.(/|$)'
    );

ALTER TABLE control_plane.session_storage
    DROP CONSTRAINT session_storage_source_relative_path_check,
    ADD CONSTRAINT session_storage_source_relative_path_check CHECK (
        source_relative_path ~ '^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[0-9a-f-]{36}\.jsonl$'
        AND source_relative_path !~ '(^|/)\.\.(/|$)'
    );

RESET ROLE;
