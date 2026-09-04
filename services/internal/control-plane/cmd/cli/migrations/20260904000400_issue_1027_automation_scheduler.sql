-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.schedules
    ADD COLUMN dst_gap_policy text NOT NULL DEFAULT 'SHIFT_FORWARD'
        CHECK (dst_gap_policy = 'SHIFT_FORWARD'),
    ADD COLUMN dst_fold_policy text NOT NULL DEFAULT 'RUN_ONCE_EARLIEST'
        CHECK (dst_fold_policy = 'RUN_ONCE_EARLIEST'),
    ADD COLUMN misfire_policy text NOT NULL DEFAULT 'COALESCE'
        CHECK (misfire_policy IN ('COALESCE', 'CATCH_UP_ONE', 'SKIP')),
    ADD COLUMN overlap_policy text NOT NULL DEFAULT 'FORBID'
        CHECK (overlap_policy IN ('FORBID', 'ALLOW')),
    ADD COLUMN target_version bigint NOT NULL DEFAULT 1 CHECK (target_version > 0),
    ADD COLUMN target_digest text NOT NULL DEFAULT repeat('0', 64)
        CHECK (target_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN automation_text text NOT NULL DEFAULT 'i18n:SCHEDULED_RUN_TASK'
        CHECK (char_length(automation_text) BETWEEN 1 AND 32768),
    ADD COLUMN prompt_inputs jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(prompt_inputs) = 'object' AND octet_length(prompt_inputs::text) <= 65536);

ALTER TABLE control_plane.schedule_revisions
    ADD COLUMN dst_gap_policy text NOT NULL DEFAULT 'SHIFT_FORWARD'
        CHECK (dst_gap_policy = 'SHIFT_FORWARD'),
    ADD COLUMN dst_fold_policy text NOT NULL DEFAULT 'RUN_ONCE_EARLIEST'
        CHECK (dst_fold_policy = 'RUN_ONCE_EARLIEST'),
    ADD COLUMN misfire_policy text NOT NULL DEFAULT 'COALESCE'
        CHECK (misfire_policy IN ('COALESCE', 'CATCH_UP_ONE', 'SKIP')),
    ADD COLUMN overlap_policy text NOT NULL DEFAULT 'FORBID'
        CHECK (overlap_policy IN ('FORBID', 'ALLOW')),
    ADD COLUMN target_version bigint NOT NULL DEFAULT 1 CHECK (target_version > 0),
    ADD COLUMN target_digest text NOT NULL DEFAULT repeat('0', 64)
        CHECK (target_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN automation_text text NOT NULL DEFAULT 'i18n:SCHEDULED_RUN_TASK'
        CHECK (char_length(automation_text) BETWEEN 1 AND 32768),
    ADD COLUMN prompt_inputs jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(prompt_inputs) = 'object' AND octet_length(prompt_inputs::text) <= 65536);

ALTER TABLE control_plane.schedule_occurrences
    DROP CONSTRAINT schedule_occurrences_state_check,
    ADD CONSTRAINT schedule_occurrences_state_check CHECK (state IN (
        'DUE', 'CLAIMED', 'MATERIALIZED', 'COMPLETED', 'FAILED', 'CANCELLED',
        'RETRY_WAIT', 'DEAD_LETTER', 'SKIPPED'
    )),
    ADD COLUMN target_version bigint NOT NULL DEFAULT 1 CHECK (target_version > 0),
    ADD COLUMN target_digest text NOT NULL DEFAULT repeat('0', 64)
        CHECK (target_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN automation_text text NOT NULL DEFAULT 'i18n:SCHEDULED_RUN_TASK'
        CHECK (char_length(automation_text) BETWEEN 1 AND 32768),
    ADD COLUMN automation_text_digest text NOT NULL DEFAULT repeat('0', 64)
        CHECK (automation_text_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN prompt_inputs jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(prompt_inputs) = 'object' AND octet_length(prompt_inputs::text) <= 65536),
    ADD COLUMN prompt_inputs_digest text NOT NULL DEFAULT repeat('0', 64)
        CHECK (prompt_inputs_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN initiated_by uuid REFERENCES control_plane.subjects(id),
    ADD COLUMN safe_error_code text NOT NULL DEFAULT ''
        CHECK (safe_error_code = '' OR safe_error_code ~ '^[A-Z0-9_]{1,64}$'),
    ADD COLUMN completed_at timestamptz,
    ADD COLUMN dead_lettered_at timestamptz;

-- Старые leases не имеют credential/attempt binding нового протокола.
UPDATE control_plane.schedule_occurrences
SET state = 'CANCELLED', completed_at = clock_timestamp(),
    safe_error_code = 'SCHEDULE_PROTOCOL_UPGRADED'
WHERE state IN ('DUE', 'CLAIMED');
UPDATE control_plane.schedule_occurrences
SET completed_at = COALESCE(completed_at, updated_at)
WHERE state IN ('COMPLETED', 'FAILED', 'CANCELLED');
UPDATE control_plane.schedule_occurrences
SET lease_ref = NULL, fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL
WHERE state <> 'CLAIMED';

-- Не переписываем прежние immutable revisions: новая фиксирует target на
-- момент перехода протокола. Неразрешимый target остаётся выключенным.
WITH targets AS (
    SELECT schedule.id AS schedule_id, agent.version AS target_version,
           encode(digest(convert_to(agent.ref || chr(31) || agent.version::text, 'UTF8'), 'sha256'), 'hex') AS target_digest
    FROM control_plane.schedules schedule
    JOIN control_plane.agents agent ON agent.ref = schedule.target_ref
      AND agent.organization_id = schedule.organization_id AND agent.project_id = schedule.project_id
    WHERE schedule.target_type = 'AGENT' AND agent.system_key IS NULL
    UNION ALL
    SELECT schedule.id, workflow.published_version::bigint, version.digest
    FROM control_plane.schedules schedule
    JOIN control_plane.workflows workflow ON workflow.ref = schedule.target_ref
      AND workflow.organization_id = schedule.organization_id AND workflow.project_id = schedule.project_id
    JOIN control_plane.workflow_versions version ON version.workflow_id = workflow.id
      AND version.version_number = workflow.published_version
    WHERE schedule.target_type = 'WORKFLOW'
), snapshots AS (
    SELECT revision.*, targets.target_version AS pinned_version, targets.target_digest AS pinned_digest,
           left(COALESCE(NULLIF(btrim(revision.input->>'task'), ''), revision.name), 32768) AS task_text
    FROM control_plane.schedules schedule
    JOIN control_plane.schedule_revisions revision ON revision.id = schedule.current_revision_id
    JOIN targets ON targets.schedule_id = schedule.id
), inserted AS (
    INSERT INTO control_plane.schedule_revisions (
        ref, organization_id, schedule_id, revision, name, target_type, target_ref, preset,
        cron_expression, timezone, input, session_policy, notification_policy, digest, created_by,
        target_version, target_digest, automation_text
    )
    SELECT 'srev_' || replace(gen_random_uuid()::text, '-', ''), organization_id, schedule_id,
           revision + 1, name, target_type, target_ref, preset, cron_expression, timezone,
           input, session_policy, notification_policy,
           encode(digest(convert_to(jsonb_build_object('protocol', 'schedule-v2',
               'previousDigest', digest, 'targetVersion', pinned_version, 'targetDigest', pinned_digest,
               'automationText', task_text, 'promptInputs', '{}'::jsonb, 'dstGapPolicy', 'SHIFT_FORWARD',
               'dstFoldPolicy', 'RUN_ONCE_EARLIEST', 'misfirePolicy', 'COALESCE', 'overlapPolicy', 'FORBID')::text,
               'UTF8'), 'sha256'), 'hex'), created_by, pinned_version, pinned_digest, task_text
    FROM snapshots
    RETURNING id, schedule_id, target_version, target_digest, automation_text
)
UPDATE control_plane.schedules schedule
SET current_revision_id = inserted.id, target_version = inserted.target_version,
    target_digest = inserted.target_digest, automation_text = inserted.automation_text,
    version = schedule.version + 1, updated_at = clock_timestamp()
FROM inserted WHERE inserted.schedule_id = schedule.id;

UPDATE control_plane.schedules SET enabled = false, next_run_at = NULL,
    version = version + 1, updated_at = clock_timestamp()
WHERE target_digest = repeat('0', 64) AND enabled;

-- Для уже созданных Run задача берётся из сохранённого Run, а не из нового target.
UPDATE control_plane.schedule_occurrences occurrence
SET automation_text = left(COALESCE(NULLIF(run.task, ''), occurrence.run_name), 32768)
FROM control_plane.runs run WHERE run.id = occurrence.run_id;
UPDATE control_plane.schedule_occurrences
SET automation_text_digest = encode(digest(convert_to(automation_text, 'UTF8'), 'sha256'), 'hex'),
    prompt_inputs_digest = encode(digest(convert_to(prompt_inputs::text, 'UTF8'), 'sha256'), 'hex');

ALTER TABLE control_plane.schedule_occurrences
    ADD CONSTRAINT schedule_occurrence_lease_complete CHECK (
        (state = 'CLAIMED' AND lease_ref IS NOT NULL AND fence_digest ~ '^[a-f0-9]{64}$'
         AND workload_instance IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (state <> 'CLAIMED' AND lease_ref IS NULL AND fence_digest IS NULL
         AND workload_instance IS NULL AND lease_expires_at IS NULL)
    ),
    ADD CONSTRAINT schedule_occurrence_terminal_time CHECK (
        (state IN ('COMPLETED','FAILED','CANCELLED','DEAD_LETTER','SKIPPED')) = (completed_at IS NOT NULL)
    ),
    ADD CONSTRAINT schedule_occurrence_dead_letter_time CHECK (
        (state = 'DEAD_LETTER') = (dead_lettered_at IS NOT NULL)
    );

UPDATE control_plane.schedule_occurrences occurrence
SET initiated_by = schedule.created_by
FROM control_plane.schedules schedule
WHERE schedule.id = occurrence.schedule_id;
ALTER TABLE control_plane.schedule_occurrences ALTER COLUMN initiated_by SET NOT NULL;

CREATE TABLE control_plane.schedule_occurrence_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^satt_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    occurrence_id uuid NOT NULL REFERENCES control_plane.schedule_occurrences(id),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    generation bigint NOT NULL CHECK (generation > 0),
    credential_generation bigint NOT NULL CHECK (credential_generation > 0),
    lease_ref text NOT NULL,
    fence_digest text NOT NULL CHECK (fence_digest ~ '^[a-f0-9]{64}$'),
    workload_instance text NOT NULL CHECK (char_length(workload_instance) BETWEEN 1 AND 128),
    state text NOT NULL CHECK (state IN ('CLAIMED','EXPIRED','MATERIALIZED','FAILED','CANCELLED')),
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    schedule_revision_digest text NOT NULL CHECK (schedule_revision_digest ~ '^[a-f0-9]{64}$'),
    run_id uuid REFERENCES control_plane.runs(id),
    session_id uuid REFERENCES control_plane.sessions(id),
    turn_id uuid REFERENCES control_plane.session_turns(id),
    issued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    safe_error_code text NOT NULL DEFAULT '' CHECK (safe_error_code = '' OR safe_error_code ~ '^[A-Z0-9_]{1,64}$'),
    UNIQUE (occurrence_id, attempt),
    UNIQUE (occurrence_id, generation),
    CHECK (expires_at > issued_at),
    CHECK ((state = 'CLAIMED') = (completed_at IS NULL)),
    CHECK ((state = 'MATERIALIZED') = (run_id IS NOT NULL AND session_id IS NOT NULL AND turn_id IS NOT NULL))
);

CREATE INDEX schedule_occurrence_attempts_active
    ON control_plane.schedule_occurrence_attempts (organization_id, expires_at, occurrence_id)
    WHERE state = 'CLAIMED';

CREATE TRIGGER protect_schedule_occurrence_attempt
BEFORE DELETE ON control_plane.schedule_occurrence_attempts
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

GRANT SELECT, INSERT, UPDATE ON control_plane.schedule_occurrence_attempts TO control_plane_runtime;

-- Отмена из любого owner path закрывает scheduler lease и историю attempts
-- в той же транзакции, включая отключение целевого Agent/Workflow.
-- +goose StatementBegin
CREATE FUNCTION control_plane.cancel_schedule_occurrence_attempt() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state = 'CANCELLED' AND OLD.state <> 'CANCELLED' THEN
        NEW.completed_at := clock_timestamp();
        NEW.dead_lettered_at := NULL;
        UPDATE control_plane.schedule_occurrence_attempts
        SET state = 'CANCELLED', completed_at = clock_timestamp()
        WHERE occurrence_id = NEW.id AND state = 'CLAIMED';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER cancel_schedule_occurrence_attempt
BEFORE UPDATE ON control_plane.schedule_occurrences
FOR EACH ROW EXECUTE FUNCTION control_plane.cancel_schedule_occurrence_attempt();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_schedule_attempt_binding() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.state <> 'CLAIMED' OR
       (to_jsonb(NEW) - ARRAY['state','expires_at','completed_at','safe_error_code','run_id','session_id','turn_id'])
       IS DISTINCT FROM
       (to_jsonb(OLD) - ARRAY['state','expires_at','completed_at','safe_error_code','run_id','session_id','turn_id']) THEN
        RAISE EXCEPTION 'schedule attempt binding is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_schedule_attempt_binding
BEFORE UPDATE ON control_plane.schedule_occurrence_attempts
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_schedule_attempt_binding();

RESET ROLE;

-- +goose Down
-- Только вперёд: immutable revisions и историю attempts удалять нельзя.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 20260904000400 is forward-only: schedule provenance cannot be discarded'
        USING ERRCODE = '0A000';
END;
$$;
-- +goose StatementEnd
