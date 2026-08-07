-- +goose Up
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'control_plane_migration') THEN
        CREATE ROLE control_plane_migration
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
END
$roles$;
ALTER ROLE control_plane_migration
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.legacy_data_cutovers (
    plan_id text PRIMARY KEY CHECK (plan_id ~ '^[a-z0-9][a-z0-9._-]{15,127}$'),
    plan_sha256 text NOT NULL CHECK (plan_sha256 ~ '^[a-f0-9]{64}$'),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    target_sha256 text NOT NULL CHECK (target_sha256 ~ '^[a-f0-9]{64}$'),
    backup_sha256 text NOT NULL CHECK (backup_sha256 ~ '^[a-f0-9]{64}$'),
    manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[a-f0-9]{64}$'),
    mapping_counts jsonb NOT NULL CHECK (
        jsonb_typeof(mapping_counts) = 'object'
        AND octet_length(mapping_counts::text) <= 16384
    ),
    state text NOT NULL CHECK (state IN ('PREPARED', 'COMMITTED', 'ABORTED')),
    prepared_at timestamptz NOT NULL,
    restore_verified_at timestamptz,
    committed_at timestamptz,
    aborted_at timestamptz,
    CHECK ((state = 'COMMITTED') = (committed_at IS NOT NULL)),
    CHECK (state <> 'COMMITTED' OR restore_verified_at IS NOT NULL),
    CHECK ((state = 'ABORTED') = (aborted_at IS NOT NULL))
);
CREATE UNIQUE INDEX legacy_data_cutovers_one_winner_uidx
    ON control_plane.legacy_data_cutovers ((true))
    WHERE state = 'COMMITTED';
ALTER TABLE control_plane.legacy_data_cutovers OWNER TO control_plane_owner;
REVOKE ALL ON control_plane.legacy_data_cutovers FROM PUBLIC;
GRANT USAGE ON SCHEMA control_plane TO control_plane_migration;
GRANT SELECT, INSERT, UPDATE ON control_plane.legacy_data_cutovers TO control_plane_migration;

CREATE FUNCTION control_plane.lock_legacy_cutover_resources()
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $$
BEGIN
    LOCK TABLE control_plane.resources, control_plane.turn_attempts,
        control_plane.runtime_executions, control_plane.runtime_derived_resources,
        control_plane.protected_resource_history IN SHARE MODE;
END
$$;
ALTER FUNCTION control_plane.lock_legacy_cutover_resources() OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.lock_legacy_cutover_resources() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.lock_legacy_cutover_resources() TO control_plane_migration;

-- Миграционная identity получает только readback target state. Запись в
-- resources, audit, event и runtime tables намеренно отсутствует.
CREATE POLICY resources_legacy_migration_read
    ON control_plane.resources
    FOR SELECT TO control_plane_migration
    USING (true);
GRANT SELECT ON control_plane.resources TO control_plane_migration;
CREATE POLICY turn_attempts_legacy_migration_read
    ON control_plane.turn_attempts
    FOR SELECT TO control_plane_migration
    USING (
        EXISTS (
            SELECT 1 FROM control_plane.resources AS turn
            WHERE turn.id = turn_attempts.turn_id AND turn.kind = 'TURN'
        )
    );
GRANT SELECT ON control_plane.turn_attempts TO control_plane_migration;
CREATE POLICY runtime_executions_legacy_migration_read
    ON control_plane.runtime_executions
    FOR SELECT TO control_plane_migration
    USING (
        EXISTS (
            SELECT 1 FROM control_plane.resources AS process
            WHERE process.id = runtime_executions.process_id AND process.kind = 'PROCESS_RUN'
        )
    );
GRANT SELECT ON control_plane.runtime_executions TO control_plane_migration;
CREATE POLICY runtime_derived_resources_legacy_migration_read
    ON control_plane.runtime_derived_resources
    FOR SELECT TO control_plane_migration
    USING (true);
GRANT SELECT ON control_plane.runtime_derived_resources TO control_plane_migration;
CREATE POLICY protected_resource_history_legacy_migration_read
    ON control_plane.protected_resource_history
    FOR SELECT TO control_plane_migration
    USING (true);
GRANT SELECT ON control_plane.protected_resource_history TO control_plane_migration;

UPDATE control_plane.schema_state
SET version = 20260807019600, migrated_at = clock_timestamp()
WHERE singleton = true;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 20260807019600 is forward-only: cutover receipt cannot be removed safely';
END
$$;
-- +goose StatementEnd
