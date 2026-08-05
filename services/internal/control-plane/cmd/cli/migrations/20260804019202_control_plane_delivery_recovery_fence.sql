-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.runtime_executions
    ADD COLUMN codex_delivery_recovery_source_execution_id uuid,
    ADD CONSTRAINT runtime_executions_delivery_recovery_source_check CHECK (
        codex_delivery_recovery_source_execution_id IS NULL OR attempt > 1
    ),
    ADD CONSTRAINT runtime_executions_delivery_recovery_source_fk
        FOREIGN KEY (codex_delivery_recovery_source_execution_id)
        REFERENCES control_plane.runtime_executions(id)
        ON DELETE RESTRICT;

RESET ROLE;

-- +goose Down
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.runtime_executions
    DROP CONSTRAINT runtime_executions_delivery_recovery_source_fk,
    DROP CONSTRAINT runtime_executions_delivery_recovery_source_check,
    DROP COLUMN codex_delivery_recovery_source_execution_id;

RESET ROLE;
