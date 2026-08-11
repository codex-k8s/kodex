-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- ScheduledRun сохраняет исходную попытку и отдельную текущую server-owned
-- execution binding для retry/continuation без перезаписи исходной истории.

ALTER TABLE control_plane.scheduled_runs
    ADD COLUMN current_session_id uuid REFERENCES control_plane.resources (id),
    ADD COLUMN current_session_version bigint,
    ADD COLUMN current_turn_id uuid REFERENCES control_plane.resources (id),
    ADD COLUMN current_turn_version bigint,
    ADD COLUMN current_turn_attempt integer,
    ADD COLUMN current_process_run_id uuid REFERENCES control_plane.resources (id),
    ADD COLUMN current_process_version bigint,
    ADD COLUMN current_runtime_revision_id uuid REFERENCES control_plane.resources (id),
    ADD COLUMN current_runtime_revision_version bigint,
    ADD COLUMN current_input_sha256 text;

UPDATE control_plane.scheduled_runs AS run
SET current_session_id = coalesce(occurrence.execution_session_id, run.session_id),
    current_session_version =
        coalesce(occurrence.execution_session_version, run.session_version),
    current_turn_id = coalesce(occurrence.execution_turn_id, run.continuation_turn_id, run.turn_id),
    current_turn_version =
        coalesce(occurrence.execution_turn_version, run.continuation_turn_version, run.turn_version),
    current_turn_attempt = (turn_resource.spec ->> 'attempt')::integer,
    current_process_run_id =
        coalesce(occurrence.execution_process_run_id, run.process_run_id),
    current_process_version =
        coalesce(occurrence.execution_process_version, run.process_version),
    current_runtime_revision_id = coalesce(
        occurrence.execution_runtime_revision_id,
        run.continuation_runtime_revision_id,
        run.runtime_revision_id
    ),
    current_runtime_revision_version = coalesce(
        occurrence.execution_runtime_revision_version,
        run.continuation_runtime_revision_version,
        run.runtime_revision_version
    ),
    current_input_sha256 = coalesce(
        occurrence.effective_input_sha256,
        run.continuation_input_sha256,
        run.effective_input_sha256
    )
FROM control_plane.schedule_occurrences AS occurrence,
     control_plane.resources AS turn_resource
WHERE occurrence.id = run.occurrence_id
  AND occurrence.attempt = run.attempt
  AND turn_resource.id = coalesce(
      occurrence.execution_turn_id,
      run.continuation_turn_id,
      run.turn_id
  )
  AND turn_resource.kind = 'TURN';

ALTER TABLE control_plane.scheduled_runs
    ALTER COLUMN current_session_id SET NOT NULL,
    ALTER COLUMN current_session_version SET NOT NULL,
    ALTER COLUMN current_turn_id SET NOT NULL,
    ALTER COLUMN current_turn_version SET NOT NULL,
    ALTER COLUMN current_turn_attempt SET NOT NULL,
    ALTER COLUMN current_runtime_revision_id SET NOT NULL,
    ALTER COLUMN current_runtime_revision_version SET NOT NULL,
    ALTER COLUMN current_input_sha256 SET NOT NULL,
    ADD CONSTRAINT scheduled_run_current_execution_complete CHECK (
        current_session_version > 0
        AND current_turn_version > 0
        AND current_turn_attempt BETWEEN 1 AND 100
        AND current_runtime_revision_version > 0
        AND current_input_sha256 ~ '^[a-f0-9]{64}$'
        AND ((current_process_run_id IS NULL AND current_process_version IS NULL)
             OR (current_process_run_id IS NOT NULL AND current_process_version > 0))
    );

CREATE UNIQUE INDEX scheduled_runs_active_current_turn_idx
    ON control_plane.scheduled_runs (current_turn_id)
    WHERE state IN ('CLAIMED', 'WAITING_OWNER', 'CONTINUATION');

UPDATE control_plane.schema_state
SET version = 20260801000100, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260801000100 is forward-only: current execution evidence cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
