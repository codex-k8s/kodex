-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.worker_grant_high_watermarks
    DROP CONSTRAINT worker_grant_high_watermarks_workload_id_check,
    ADD CONSTRAINT worker_grant_high_watermarks_workload_id_check
        CHECK (workload_id IN (
            'automation-scheduler',
            'image-admission',
            'image-promotion',
            'integration-gateway',
            'interaction-gateway',
            'role-image-builder',
            'runtime-controller',
            'secret-broker',
            'session-archive'
        ));

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DELETE FROM control_plane.worker_grant_high_watermarks
WHERE workload_id IN ('interaction-gateway', 'secret-broker', 'session-archive');

ALTER TABLE control_plane.worker_grant_high_watermarks
    DROP CONSTRAINT worker_grant_high_watermarks_workload_id_check,
    ADD CONSTRAINT worker_grant_high_watermarks_workload_id_check
        CHECK (workload_id IN (
            'automation-scheduler',
            'image-admission',
            'image-promotion',
            'integration-gateway',
            'role-image-builder',
            'runtime-controller'
        ));

RESET ROLE;
