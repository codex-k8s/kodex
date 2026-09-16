-- +goose Up
SET ROLE control_plane_owner;

-- Это поколение серверного допуска, не сертификат и не task grant.
-- Реестр используется только явно выбранным trusted-cluster reader.
CREATE TABLE control_plane.trusted_workload_generations (
    workload_id text PRIMARY KEY CHECK (workload_id IN (
        'automation-scheduler', 'control-plane', 'email-bridge',
        'image-admission', 'image-promotion', 'integration-gateway',
        'interaction-gateway', 'role-image-builder', 'runtime-controller',
        'secret-broker', 'session-archive', 'stt-tts-service'
    )),
    credential_generation bigint NOT NULL CHECK (credential_generation BETWEEN 1 AND 9007199254740991),
    enabled boolean NOT NULL DEFAULT true
);

INSERT INTO control_plane.trusted_workload_generations (workload_id, credential_generation)
SELECT registry.workload_id, GREATEST(1, previous.credential_generation)
FROM (VALUES
    ('automation-scheduler'), ('control-plane'), ('email-bridge'),
    ('image-admission'), ('image-promotion'), ('integration-gateway'),
    ('interaction-gateway'), ('role-image-builder'), ('runtime-controller'),
    ('secret-broker'), ('session-archive'), ('stt-tts-service')
) AS registry(workload_id)
LEFT JOIN control_plane.worker_grant_high_watermarks AS previous USING (workload_id);

REVOKE ALL ON control_plane.trusted_workload_generations FROM PUBLIC, control_plane_runtime;
GRANT SELECT ON control_plane.trusted_workload_generations TO control_plane_runtime;
ALTER TABLE control_plane.trusted_workload_generations ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.trusted_workload_generations FORCE ROW LEVEL SECURITY;
-- Реестр общий для установки, не содержит tenant-данных; runtime только читает.
CREATE POLICY trusted_workload_reader ON control_plane.trusted_workload_generations
    FOR SELECT TO control_plane_runtime USING (true);
CREATE POLICY trusted_workload_owner ON control_plane.trusted_workload_generations
    TO control_plane_owner USING (true) WITH CHECK (true);

-- +goose StatementBegin
CREATE FUNCTION control_plane.check_trusted_workload_generation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
BEGIN
    IF NEW.workload_id <> OLD.workload_id OR
       NEW.credential_generation < OLD.credential_generation OR
       (NOT OLD.enabled AND NEW.enabled AND NEW.credential_generation <= OLD.credential_generation) THEN
        RAISE EXCEPTION 'trusted workload generation must advance' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.check_trusted_workload_generation() FROM PUBLIC, control_plane_runtime;
CREATE TRIGGER trusted_workload_generation_forward_only
    BEFORE UPDATE ON control_plane.trusted_workload_generations
    FOR EACH ROW EXECUTE FUNCTION control_plane.check_trusted_workload_generation();

RESET ROLE;
