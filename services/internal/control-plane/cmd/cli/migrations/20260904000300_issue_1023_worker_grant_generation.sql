-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.worker_grant_high_watermarks
    ADD COLUMN credential_generation bigint NOT NULL DEFAULT 1
        CHECK (credential_generation > 0);

ALTER TABLE control_plane.worker_grant_high_watermarks
    ALTER COLUMN credential_generation DROP DEFAULT;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.worker_grant_high_watermarks
    DROP COLUMN credential_generation;

RESET ROLE;
