-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Owner eligibility применяется внутри authoritative SQL до cursor LIMIT.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE INDEX resources_owner_scope_kind_page_idx
    ON control_plane.resources (
        organization_id, project_id, kind, owner_actor_id, id
    )
    WHERE state <> 'DELETED'
      AND kind IN ('SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE', 'OWNER_GATE', 'WORK_CLAIM');

CREATE INDEX runtime_execution_incidents_scope_page_idx
    ON control_plane.runtime_execution_incidents (
        organization_id, project_id, id, execution_id
    );

UPDATE control_plane.schema_state
SET version = 20260804019200, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 20260804019200 is forward-only: owner eligibility indexes are part of the protected read path';
END
$$;
-- +goose StatementEnd
