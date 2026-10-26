-- name: project_purge_authority_cleanup_set_role :exec
-- Роль принадлежит только закрытому одноразовому fixture текущей БД.
DROP OWNED BY purge_authority_probe_writer;
DROP ROLE purge_authority_probe_writer;
