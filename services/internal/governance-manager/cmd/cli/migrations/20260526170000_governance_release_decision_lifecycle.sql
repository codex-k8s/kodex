-- +goose Up
ALTER TABLE governance_manager_release_decisions
    DROP CONSTRAINT governance_manager_release_decisions_outcome_chk;

UPDATE governance_manager_release_decisions
SET outcome = 'hold'
WHERE outcome = '';

ALTER TABLE governance_manager_release_decisions
    ADD CONSTRAINT governance_manager_release_decisions_outcome_chk
        CHECK (outcome IN ('', 'go', 'go_with_conditions', 'no_go', 'hold', 'rollback', 'follow_up_required'));

ALTER TABLE governance_manager_release_decisions
    ADD COLUMN created_at timestamptz,
    ADD COLUMN updated_at timestamptz;

UPDATE governance_manager_release_decisions
SET created_at = decided_at,
    updated_at = decided_at
WHERE created_at IS NULL
   OR updated_at IS NULL;

ALTER TABLE governance_manager_release_decisions
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;

CREATE UNIQUE INDEX governance_manager_release_decisions_package_uidx
    ON governance_manager_release_decisions (release_decision_package_id);

CREATE UNIQUE INDEX governance_manager_release_safety_states_package_uidx
    ON governance_manager_release_safety_states (release_decision_package_id);

CREATE INDEX governance_manager_blocking_signals_target_status_idx
    ON governance_manager_blocking_signals (target_type, target_ref, status, severity, created_at DESC, id);

ALTER TABLE governance_manager_blocking_signals
    ADD COLUMN updated_at timestamptz;

UPDATE governance_manager_blocking_signals
SET updated_at = created_at
WHERE updated_at IS NULL;

ALTER TABLE governance_manager_blocking_signals
    ALTER COLUMN updated_at SET NOT NULL;

-- +goose Down
ALTER TABLE governance_manager_blocking_signals
    DROP COLUMN IF EXISTS updated_at;

DROP INDEX IF EXISTS governance_manager_blocking_signals_target_status_idx;
DROP INDEX IF EXISTS governance_manager_release_safety_states_package_uidx;
DROP INDEX IF EXISTS governance_manager_release_decisions_package_uidx;

ALTER TABLE governance_manager_release_decisions
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at;

ALTER TABLE governance_manager_release_decisions
    DROP CONSTRAINT governance_manager_release_decisions_outcome_chk;

ALTER TABLE governance_manager_release_decisions
    ADD CONSTRAINT governance_manager_release_decisions_outcome_chk
        CHECK (outcome IN ('go', 'go_with_conditions', 'no_go', 'hold', 'rollback', 'follow_up_required'));
