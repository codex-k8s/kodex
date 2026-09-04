-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.interaction_deliveries
    DROP CONSTRAINT interaction_deliveries_state_check,
    ADD CONSTRAINT interaction_deliveries_state_check
    CHECK (state IN ('DUE','CLAIMED','SUCCEEDED','FAILED','CANCELLED','UNKNOWN_OUTCOME'));
UPDATE control_plane.interaction_deliveries
SET state = 'UNKNOWN_OUTCOME', safe_error_code = 'INTERACTION_OUTCOME_UNKNOWN',
    version = version + 1, updated_at = clock_timestamp()
WHERE state = 'FAILED';
RESET ROLE;
