-- +goose Up
SET ROLE control_plane_owner;

-- Только forward-допуск нового worker; прежние поколения, watermark и ACL не меняются.
LOCK TABLE control_plane.trusted_workload_generations,
    control_plane.worker_grant_high_watermarks IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $migration$
BEGIN
    IF (SELECT pg_get_constraintdef(oid) FROM pg_constraint
        WHERE conrelid = 'control_plane.trusted_workload_generations'::regclass
          AND conname = 'trusted_workload_generations_workload_id_check') IS DISTINCT FROM
        'CHECK ((workload_id = ANY (ARRAY[''automation-scheduler''::text, ''control-plane''::text, ''email-bridge''::text, ''image-admission''::text, ''image-admission-controller''::text, ''image-promotion''::text, ''integration-gateway''::text, ''interaction-gateway''::text, ''role-image-builder''::text, ''runtime-controller''::text, ''secret-broker''::text, ''session-archive''::text, ''stt-tts-service''::text])))'
       OR (SELECT pg_get_constraintdef(oid) FROM pg_constraint
        WHERE conrelid = 'control_plane.worker_grant_high_watermarks'::regclass
          AND conname = 'worker_grant_high_watermarks_workload_id_check') IS DISTINCT FROM
        'CHECK ((workload_id = ANY (ARRAY[''automation-scheduler''::text, ''control-plane''::text, ''email-bridge''::text, ''image-admission''::text, ''image-promotion''::text, ''integration-gateway''::text, ''interaction-gateway''::text, ''role-image-builder''::text, ''runtime-controller''::text, ''secret-broker''::text, ''session-archive''::text])))'
       OR EXISTS (SELECT 1 FROM control_plane.trusted_workload_generations WHERE workload_id = 'artifact-retention')
       OR EXISTS (SELECT 1 FROM control_plane.worker_grant_high_watermarks WHERE workload_id = 'artifact-retention') THEN
        RAISE EXCEPTION 'artifact retention grant predecessor rejected' USING ERRCODE = '55000';
    END IF;
END
$migration$;
-- +goose StatementEnd

ALTER TABLE control_plane.trusted_workload_generations
    DROP CONSTRAINT trusted_workload_generations_workload_id_check,
    ADD CONSTRAINT trusted_workload_generations_workload_id_check CHECK (workload_id IN (
        'automation-scheduler', 'control-plane', 'email-bridge',
        'image-admission', 'image-admission-controller', 'image-promotion',
        'integration-gateway', 'interaction-gateway', 'role-image-builder',
        'runtime-controller', 'secret-broker', 'session-archive', 'stt-tts-service',
        'artifact-retention'
    ));
ALTER TABLE control_plane.worker_grant_high_watermarks
    DROP CONSTRAINT worker_grant_high_watermarks_workload_id_check,
    ADD CONSTRAINT worker_grant_high_watermarks_workload_id_check CHECK (workload_id IN (
        'automation-scheduler', 'control-plane', 'email-bridge',
        'image-admission', 'image-promotion', 'integration-gateway',
        'interaction-gateway', 'role-image-builder', 'runtime-controller',
        'secret-broker', 'session-archive', 'artifact-retention'
    ));

INSERT INTO control_plane.trusted_workload_generations
    (workload_id, credential_generation, enabled)
VALUES ('artifact-retention', 1, true);

RESET ROLE;
