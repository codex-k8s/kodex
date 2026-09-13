-- +goose Up
SET ROLE control_plane_owner;

-- Trigger выполняется с правами artifact-retention. Вызванная им функция
-- читает несколько owner tables, поэтому оставляем наружу только один bounded
-- счётчик, а таблицы не выдаём технической роли напрямую.
ALTER FUNCTION control_plane.skill_artifact_reference_count(uuid,text,bigint,text)
    SECURITY DEFINER
    SET search_path = pg_catalog, control_plane;
GRANT EXECUTE ON FUNCTION control_plane.skill_artifact_reference_count(uuid,text,bigint,text)
    TO artifact_retention_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
REVOKE EXECUTE ON FUNCTION control_plane.skill_artifact_reference_count(uuid,text,bigint,text)
    FROM artifact_retention_runtime;
ALTER FUNCTION control_plane.skill_artifact_reference_count(uuid,text,bigint,text)
    SECURITY INVOKER
    RESET search_path;
RESET ROLE;
