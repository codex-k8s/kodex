-- +goose Up
-- CHANGES_REQUESTED продолжает тот же ProcessRun через новый неизменяемый
-- turn/revision, сохраняя первоначальную binding scheduled_runs.
RESET ROLE;

ALTER TABLE control_plane.scheduled_runs
    ADD COLUMN continuation_turn_id uuid REFERENCES control_plane.resources (id),
    ADD COLUMN continuation_turn_version bigint,
    ADD COLUMN continuation_runtime_revision_id uuid REFERENCES control_plane.resources (id),
    ADD COLUMN continuation_runtime_revision_version bigint,
    ADD COLUMN continuation_input_sha256 text,
    ADD COLUMN owner_feedback_sha256 text,
    ADD CONSTRAINT scheduled_run_continuation_complete CHECK (
        (continuation_turn_id IS NULL
         AND continuation_turn_version IS NULL
         AND continuation_runtime_revision_id IS NULL
         AND continuation_runtime_revision_version IS NULL
         AND continuation_input_sha256 IS NULL
         AND owner_feedback_sha256 IS NULL)
        OR
        (continuation_turn_id IS NOT NULL
         AND continuation_turn_version > 0
         AND continuation_runtime_revision_id IS NOT NULL
         AND continuation_runtime_revision_version > 0
         AND continuation_input_sha256 ~ '^[a-f0-9]{64}$'
         AND owner_feedback_sha256 ~ '^[a-f0-9]{64}$')
    );

ALTER TABLE control_plane.schedule_occurrences
    DROP CONSTRAINT schedule_occurrences_state_check;
ALTER TABLE control_plane.schedule_occurrences
    ADD CONSTRAINT schedule_occurrences_state_check CHECK (state IN (
        'QUEUED', 'CLAIMED', 'WAITING_OWNER', 'CONTINUATION', 'SUCCEEDED',
        'FAILED', 'CANCELLED', 'SKIPPED', 'DEAD_LETTER'
    ));

ALTER TABLE control_plane.scheduled_runs
    DROP CONSTRAINT scheduled_runs_state_check,
    DROP CONSTRAINT scheduled_runs_finished_consistency;
ALTER TABLE control_plane.scheduled_runs
    ADD CONSTRAINT scheduled_runs_state_check CHECK (state IN (
        'CLAIMED', 'WAITING_OWNER', 'CONTINUATION', 'SUCCEEDED', 'FAILED',
        'CANCELLED'
    )),
    ADD CONSTRAINT scheduled_runs_finished_consistency CHECK (
        (state IN ('CLAIMED', 'WAITING_OWNER', 'CONTINUATION')) =
        (finished_at IS NULL)
    );

UPDATE control_plane.schema_state
SET version = 20260731000600, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260731000600 is forward-only: continuation lineage and scheduled execution evidence cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
