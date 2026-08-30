-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.provider_accounts
    ADD COLUMN max_concurrent_executions integer NOT NULL DEFAULT 1
        CHECK (max_concurrent_executions BETWEEN 1 AND 256);

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.provider_accounts
    DROP COLUMN max_concurrent_executions;

RESET ROLE;
