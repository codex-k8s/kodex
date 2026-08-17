-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- safe_diagnostics имеет объявленный double precision contract. PostgreSQL
-- возвращает extract(epoch ...) как numeric, поэтому тип приводится явно.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.safe_diagnostics()
RETURNS TABLE (
    schema_version bigint,
    pending_outbox_events bigint,
    terminal_outbox_events bigint,
    oldest_pending_seconds double precision,
    active_turn_leases bigint,
    queued_schedule_occurrences bigint,
    runtime_principal_status text,
    runtime_principal_generation bigint
)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    verified_scope record;
BEGIN
    SELECT * INTO verified_scope FROM control_plane.runtime_scope();
    RETURN QUERY
    SELECT
        state.version,
        count(*) FILTER (
            WHERE outbox.published_at IS NULL AND NOT outbox.terminal
        ),
        count(*) FILTER (
            WHERE outbox.published_at IS NULL AND outbox.terminal
        ),
        coalesce(max(
            extract(epoch FROM clock_timestamp() - outbox.occurred_at)
        ) FILTER (
            WHERE outbox.published_at IS NULL AND NOT outbox.terminal
        ), 0)::double precision,
        (
            SELECT count(*)
              FROM control_plane.turn_leases AS lease
             WHERE lease.expires_at > clock_timestamp()
        ),
        (
            SELECT count(*)
              FROM control_plane.schedule_occurrences AS occurrence
             WHERE occurrence.organization_id = verified_scope.organization_id
               AND occurrence.project_id = verified_scope.project_id
               AND occurrence.state = 'QUEUED'
        ),
        principal.status,
        principal.generation
    FROM control_plane.schema_state AS state
    JOIN control_plane.runtime_principals AS principal
      ON principal.principal_name::text = session_user
    LEFT JOIN control_plane.outbox_events AS outbox
      ON outbox.organization_id = verified_scope.organization_id
     AND outbox.project_id = verified_scope.project_id
    WHERE state.singleton = true
    GROUP BY state.version, principal.status, principal.generation;
END
$function$;
-- +goose StatementEnd

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

    IF stored_version IS DISTINCT FROM 20260817000100 THEN
        RAISE EXCEPTION 'control-plane owner read repair source version is invalid'
            USING ERRCODE = '55000';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260817000200,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: возвращать несовместимый diagnostics contract запрещено.
SELECT 1;
