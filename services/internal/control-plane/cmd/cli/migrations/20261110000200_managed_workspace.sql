-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runtime_environment_versions
    ADD COLUMN managed_workspace boolean NOT NULL DEFAULT false;

RESET ROLE;

-- +goose Down
-- Forward-only: published versions and execution snapshots retain their policy.
SELECT 1;
