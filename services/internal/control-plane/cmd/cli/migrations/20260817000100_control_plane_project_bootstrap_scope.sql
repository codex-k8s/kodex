-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- Project bootstrap больше не выполняет project-owned INSERT из trigger под
-- organization scope. Application transaction после создания корневого
-- PROJECT переключается на назначенный сервером Project scope, создаёт policy,
-- audit и outbox, затем возвращается для organization-scoped receipt.
DROP TRIGGER resources_default_retention_policy ON control_plane.resources;
DROP FUNCTION control_plane.ensure_default_resource_retention_policy();

-- +goose StatementBegin
DO $$
DECLARE
    stored_version bigint;
BEGIN
    SELECT version
      INTO stored_version
      FROM control_plane.schema_state
     WHERE singleton = true
     FOR UPDATE;

    IF stored_version <> 20260814000300 THEN
        RAISE EXCEPTION 'control-plane project bootstrap source version is invalid'
            USING ERRCODE = '55000';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260817000100,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: trigger-based bootstrap with an organization-scoped RLS gap
-- is intentionally not restored.
SELECT 1;
