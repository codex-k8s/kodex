-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.artifacts
    ADD COLUMN lifecycle_state text NOT NULL DEFAULT 'ACTIVE'
        CHECK (lifecycle_state IN ('ACTIVE', 'DELETED', 'PURGE_PENDING', 'PURGED')),
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN purge_after timestamptz,
    ADD COLUMN purged_at timestamptz,
    ADD CONSTRAINT artifacts_lifecycle_consistency CHECK (
        (lifecycle_state = 'ACTIVE' AND deleted_at IS NULL AND purge_after IS NULL AND purged_at IS NULL)
        OR (lifecycle_state = 'DELETED' AND deleted_at IS NOT NULL AND purge_after IS NOT NULL AND purged_at IS NULL)
        OR (lifecycle_state = 'PURGE_PENDING' AND deleted_at IS NOT NULL AND purge_after IS NOT NULL AND purged_at IS NULL)
        OR (lifecycle_state = 'PURGED' AND deleted_at IS NOT NULL AND purge_after IS NOT NULL AND purged_at IS NOT NULL)
    );

CREATE INDEX artifacts_trash_retention
    ON control_plane.artifacts (purge_after, id)
    WHERE lifecycle_state IN ('DELETED', 'PURGE_PENDING');

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
DROP INDEX control_plane.artifacts_trash_retention;
ALTER TABLE control_plane.artifacts
    DROP CONSTRAINT artifacts_lifecycle_consistency,
    DROP COLUMN purged_at,
    DROP COLUMN purge_after,
    DROP COLUMN deleted_at,
    DROP COLUMN lifecycle_state;
RESET ROLE;
