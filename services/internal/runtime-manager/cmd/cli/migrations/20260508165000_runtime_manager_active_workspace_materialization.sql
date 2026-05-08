-- +goose Up
ALTER TABLE runtime_manager_slots
    ADD COLUMN active_workspace_materialization_id uuid;

CREATE INDEX runtime_manager_slots_active_workspace_materialization_idx
    ON runtime_manager_slots (active_workspace_materialization_id)
    WHERE active_workspace_materialization_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS runtime_manager_slots_active_workspace_materialization_idx;

ALTER TABLE runtime_manager_slots
    DROP COLUMN IF EXISTS active_workspace_materialization_id;
