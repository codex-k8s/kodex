-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.assistant_plan_revisions
    DROP CONSTRAINT assistant_plan_revisions_operations_check,
    ADD CONSTRAINT assistant_plan_revisions_operations_check
        CHECK (
            jsonb_typeof(operations) = 'array'
            AND jsonb_array_length(operations) BETWEEN 1 AND 32
            AND octet_length(operations::text) <= 2097152
        );

RESET ROLE;
