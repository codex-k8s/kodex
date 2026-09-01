-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.schedules
    ADD COLUMN lifecycle_state text NOT NULL DEFAULT 'ACTIVE',
    ADD CONSTRAINT schedules_lifecycle_state_check
        CHECK (lifecycle_state IN ('ACTIVE', 'ARCHIVED')),
    ADD CONSTRAINT schedules_archived_disabled_check
        CHECK (lifecycle_state <> 'ARCHIVED' OR NOT enabled);

RESET ROLE;
