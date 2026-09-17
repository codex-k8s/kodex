-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.trusted_workload_generations
    DROP CONSTRAINT trusted_workload_generations_workload_id_check;
ALTER TABLE control_plane.trusted_workload_generations
    ADD CONSTRAINT trusted_workload_generations_workload_id_check CHECK (workload_id IN (
        'automation-scheduler', 'control-plane', 'email-bridge',
        'image-admission', 'image-admission-controller', 'image-promotion',
        'integration-gateway', 'interaction-gateway', 'role-image-builder',
        'runtime-controller', 'secret-broker', 'session-archive', 'stt-tts-service'
    ));

INSERT INTO control_plane.trusted_workload_generations
    (workload_id, credential_generation, enabled)
VALUES ('image-admission-controller', 1, true)
ON CONFLICT (workload_id) DO NOTHING;

RESET ROLE;
