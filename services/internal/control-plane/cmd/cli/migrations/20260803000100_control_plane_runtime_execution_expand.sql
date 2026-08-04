-- +goose Up
-- Forward-only expand для runtime evidence. Миграция 20260802000100 уже могла
-- быть применена и поэтому остаётся byte-for-byte равной exact main.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.runtime_executions
    ADD COLUMN cleanup_pvc_name text CHECK (
        cleanup_pvc_name IS NULL OR length(cleanup_pvc_name) BETWEEN 1 AND 253
    ),
    ADD COLUMN cleanup_pvc_uid uuid,
    ADD COLUMN cleanup_pvc_resource_version text CHECK (
        cleanup_pvc_resource_version IS NULL OR length(cleanup_pvc_resource_version) BETWEEN 1 AND 64
    ),
    ADD COLUMN cleanup_claimed_at timestamptz,
    ADD COLUMN cleanup_eligible_at timestamptz,
    ADD COLUMN cleanup_not_found_at timestamptz,
    ADD COLUMN cleanup_deletion_proof_sha256 text CHECK (
        cleanup_deletion_proof_sha256 IS NULL OR cleanup_deletion_proof_sha256 ~ '^[a-f0-9]{64}$'
    ),
    ADD COLUMN restore_source_execution_id uuid,
    ADD COLUMN restore_source_archive_reference text,
    ADD COLUMN restore_source_archive_sha256 text CHECK (
        restore_source_archive_sha256 IS NULL OR restore_source_archive_sha256 ~ '^[a-f0-9]{64}$'
    ),
    ADD COLUMN restore_source_runtime_revision_sha256 text CHECK (
        restore_source_runtime_revision_sha256 IS NULL OR restore_source_runtime_revision_sha256 ~ '^[a-f0-9]{64}$'
    ),
    ADD COLUMN restore_source_immutable_input_sha256 text CHECK (
        restore_source_immutable_input_sha256 IS NULL OR restore_source_immutable_input_sha256 ~ '^[a-f0-9]{64}$'
    ),
    ADD COLUMN restore_source_proof_sha256 text CHECK (
        restore_source_proof_sha256 IS NULL OR restore_source_proof_sha256 ~ '^[a-f0-9]{64}$'
    );

DO $drop_runtime_checks$
DECLARE
    constraint_name text;
BEGIN
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'control_plane.runtime_executions'::regclass
          AND contype = 'c'
          AND (
              pg_get_constraintdef(oid) LIKE '%terminal_outcome IN (%SUCCEEDED%FAILED%SUSPENDED%CANCELLED%EXPIRED%'
              OR pg_get_constraintdef(oid) LIKE '%state = %SUCCEEDED%terminal_outcome = %SUCCEEDED%'
              OR pg_get_constraintdef(oid) LIKE '%cleanup_authorization_state = %NONE%cleanup_authorization_generation%'
          )
    LOOP
        EXECUTE format('ALTER TABLE control_plane.runtime_executions DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END
$drop_runtime_checks$;

ALTER TABLE control_plane.runtime_executions
    ADD CONSTRAINT runtime_executions_terminal_outcome_v2_ck CHECK (
        terminal_outcome IS NULL OR terminal_outcome IN (
            'SUCCEEDED', 'FAILED', 'SUSPENDED', 'CANCELLED', 'EXPIRED', 'BLOCKED'
        )
    ),
    ADD CONSTRAINT runtime_executions_terminal_state_v2_ck CHECK (
        (state = 'SUCCEEDED' AND terminal_outcome = 'SUCCEEDED')
        OR (state = 'FAILED' AND terminal_outcome = 'FAILED')
        OR (state = 'SUSPENDED' AND terminal_outcome IN ('SUSPENDED', 'BLOCKED'))
        OR (state = 'CANCELLED' AND terminal_outcome = 'CANCELLED')
        OR (state = 'EXPIRED' AND terminal_outcome = 'EXPIRED')
        OR (state = 'RETRIED' AND terminal_outcome IN ('FAILED', 'EXPIRED'))
        OR (state NOT IN ('SUCCEEDED', 'FAILED', 'SUSPENDED', 'CANCELLED', 'EXPIRED', 'RETRIED')
            AND terminal_outcome IS NULL)
    ),
    ADD CONSTRAINT runtime_executions_restore_source_v2_ck CHECK (
        (restore_source_execution_id IS NULL AND restore_source_archive_reference IS NULL
            AND restore_source_archive_sha256 IS NULL
            AND restore_source_runtime_revision_sha256 IS NULL
            AND restore_source_immutable_input_sha256 IS NULL
            AND restore_source_proof_sha256 IS NULL)
        OR (restore_source_execution_id IS NOT NULL AND restore_source_archive_reference IS NOT NULL
            AND restore_source_archive_sha256 IS NOT NULL
            AND restore_source_runtime_revision_sha256 IS NOT NULL
            AND restore_source_immutable_input_sha256 IS NOT NULL
            AND restore_source_proof_sha256 IS NOT NULL)
    ),
    ADD CONSTRAINT runtime_executions_cleanup_v2_ck CHECK (
        (cleanup_authorization_state = 'NONE'
            AND cleanup_authorization_generation = 0
            AND cleanup_authorization_id IS NULL
            AND cleanup_authorization_expires_at IS NULL
            AND cleanup_consumed_at IS NULL
            AND cleanup_pvc_name IS NULL AND cleanup_pvc_uid IS NULL
            AND cleanup_pvc_resource_version IS NULL AND cleanup_claimed_at IS NULL
            AND cleanup_eligible_at IS NULL AND cleanup_not_found_at IS NULL
            AND cleanup_deletion_proof_sha256 IS NULL)
        OR (cleanup_authorization_state = 'ACTIVE'
            AND cleanup_authorization_generation > 0
            AND cleanup_authorization_id IS NOT NULL
            AND cleanup_authorization_expires_at > updated_at
            AND cleanup_consumed_at IS NULL
            AND cleanup_pvc_name IS NOT NULL AND cleanup_pvc_uid IS NOT NULL
            AND cleanup_pvc_resource_version IS NOT NULL
            AND cleanup_claimed_at = updated_at
            AND cleanup_eligible_at <= cleanup_claimed_at
            AND cleanup_not_found_at IS NULL AND cleanup_deletion_proof_sha256 IS NULL
            AND archive_sha256 IS NOT NULL AND restore_proof_sha256 IS NOT NULL)
        OR (cleanup_authorization_state = 'EXPIRED'
            AND cleanup_authorization_generation > 0
            AND cleanup_authorization_id IS NOT NULL
            AND cleanup_authorization_expires_at <= updated_at
            AND cleanup_consumed_at IS NULL
            AND cleanup_pvc_name IS NOT NULL AND cleanup_pvc_uid IS NOT NULL
            AND cleanup_pvc_resource_version IS NOT NULL
            AND cleanup_claimed_at IS NOT NULL AND cleanup_eligible_at <= cleanup_claimed_at
            AND cleanup_not_found_at IS NULL AND cleanup_deletion_proof_sha256 IS NULL
            AND archive_sha256 IS NOT NULL AND restore_proof_sha256 IS NOT NULL)
        OR (cleanup_authorization_state = 'CONSUMED'
            AND cleanup_authorization_generation > 0
            AND cleanup_authorization_id IS NOT NULL
            AND cleanup_authorization_expires_at IS NOT NULL
            AND cleanup_consumed_at IS NOT NULL AND cleanup_consumed_at <= updated_at
            AND cleanup_pvc_name IS NOT NULL AND cleanup_pvc_uid IS NOT NULL
            AND cleanup_pvc_resource_version IS NOT NULL
            AND cleanup_claimed_at IS NOT NULL AND cleanup_eligible_at <= cleanup_claimed_at
            AND cleanup_not_found_at BETWEEN cleanup_claimed_at AND cleanup_consumed_at
            AND cleanup_deletion_proof_sha256 IS NOT NULL
            AND archive_sha256 IS NOT NULL AND restore_proof_sha256 IS NOT NULL)
    );

UPDATE control_plane.schema_state
SET version = 20260803000100, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260803000100 is forward-only: runtime evidence cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
