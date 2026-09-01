-- +goose Up
SET ROLE control_plane_owner;

-- Предикаты DELETE читают artifact_id, поэтому PostgreSQL требует узкий
-- SELECT grant в дополнение к DELETE на обе зависимые таблицы.
GRANT SELECT (artifact_id) ON TABLE control_plane.artifact_bindings,
    control_plane.artifact_download_grants
    TO artifact_retention_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
REVOKE SELECT (artifact_id) ON TABLE control_plane.artifact_bindings,
    control_plane.artifact_download_grants
    FROM artifact_retention_runtime;
RESET ROLE;
