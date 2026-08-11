-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Residual review-пакет: exact-base constraints, единый restore-source schema,
-- server-owned retention lifecycle и session hold.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

-- Имена двух anonymous CHECK определены исходной migration 20260802000100:
-- column CHECK получает runtime_executions_terminal_outcome_check, четвёртый
-- table CHECK — runtime_executions_check3. Удаление не зависит от текстового
-- представления pg_get_constraintdef и поэтому одинаково для fresh/upgrade.
-- +goose StatementBegin
DO $assert_exact_base_constraints$
DECLARE
    terminal_attnum smallint;
BEGIN
    SELECT attnum INTO STRICT terminal_attnum
    FROM pg_attribute
    WHERE attrelid = 'control_plane.runtime_executions'::regclass
      AND attname = 'terminal_outcome'
      AND NOT attisdropped;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'control_plane.runtime_executions'::regclass
          AND conname = 'runtime_executions_terminal_outcome_check'
          AND (contype <> 'c' OR conkey <> ARRAY[terminal_attnum])
    ) THEN
        RAISE EXCEPTION 'runtime_executions_terminal_outcome_check catalog identity mismatch';
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'control_plane.runtime_executions'::regclass
          AND conname = 'runtime_executions_check3'
          AND contype <> 'c'
    ) THEN
        RAISE EXCEPTION 'runtime_executions_check3 catalog identity mismatch';
    END IF;
END
$assert_exact_base_constraints$;
-- +goose StatementEnd

ALTER TABLE control_plane.runtime_executions
    DROP CONSTRAINT IF EXISTS runtime_executions_terminal_outcome_check,
    DROP CONSTRAINT IF EXISTS runtime_executions_check3,
    DROP CONSTRAINT IF EXISTS runtime_executions_terminal_outcome_v2_ck,
    DROP CONSTRAINT IF EXISTS runtime_executions_terminal_state_v2_ck;

ALTER TABLE control_plane.runtime_executions
    ADD CONSTRAINT runtime_executions_terminal_outcome_v3_ck CHECK (
        terminal_outcome IS NULL OR terminal_outcome IN (
            'SUCCEEDED', 'FAILED', 'SUSPENDED', 'CANCELLED', 'EXPIRED', 'BLOCKED'
        )
    ),
    ADD CONSTRAINT runtime_executions_terminal_state_v3_ck CHECK (
        (state = 'SUCCEEDED' AND terminal_outcome = 'SUCCEEDED')
        OR (state = 'FAILED' AND terminal_outcome = 'FAILED')
        OR (state = 'SUSPENDED' AND terminal_outcome IN ('SUSPENDED', 'BLOCKED'))
        OR (state = 'CANCELLED' AND terminal_outcome = 'CANCELLED')
        OR (state = 'EXPIRED' AND terminal_outcome = 'EXPIRED')
        OR (state = 'RETRIED' AND terminal_outcome IN ('FAILED', 'EXPIRED'))
        OR (state NOT IN ('SUCCEEDED', 'FAILED', 'SUSPENDED', 'CANCELLED', 'EXPIRED', 'RETRIED')
            AND terminal_outcome IS NULL)
    );

-- 20260803000200 ввела переходные имена. Сначала переносим значения в
-- canonical columns, затем удаляем единственный временный источник правды.
ALTER TABLE control_plane.runtime_executions
    ADD COLUMN restore_source_version bigint,
    ADD COLUMN restore_source_archive_object_key text,
    ADD COLUMN restore_source_archive_kms_key_arn text,
    ADD COLUMN restore_source_archive_object_lock_mode text;

UPDATE control_plane.runtime_executions
SET restore_source_version = restore_source_execution_version,
    restore_source_archive_object_key = restore_source_archive_key,
    restore_source_archive_kms_key_arn = restore_source_kms_key_arn,
    restore_source_archive_object_lock_mode = restore_source_object_lock_mode
WHERE restore_source_execution_id IS NOT NULL;

ALTER TABLE control_plane.runtime_executions
    DROP CONSTRAINT runtime_executions_restore_source_v3_ck,
    DROP COLUMN restore_source_execution_version,
    DROP COLUMN restore_source_archive_key,
    DROP COLUMN restore_source_kms_key_arn,
    DROP COLUMN restore_source_object_lock_mode,
    ADD CONSTRAINT runtime_executions_restore_source_v4_ck CHECK (
        (restore_source_execution_id IS NULL AND restore_source_archive_reference IS NULL
            AND restore_source_archive_sha256 IS NULL
            AND restore_source_runtime_revision_sha256 IS NULL
            AND restore_source_immutable_input_sha256 IS NULL
            AND restore_source_proof_sha256 IS NULL
            AND restore_source_version IS NULL
            AND restore_source_archive_object_key IS NULL
            AND restore_source_archive_version_id IS NULL
            AND restore_source_archive_kms_key_arn IS NULL
            AND restore_source_archive_object_lock_mode IS NULL
            AND restore_source_archive_retain_until IS NULL
            AND restore_source_retention_policy_id IS NULL
            AND restore_source_retention_policy_version IS NULL
            AND restore_source_provenance_sha256 IS NULL)
        OR (restore_source_execution_id IS NOT NULL AND restore_source_archive_reference IS NOT NULL
            AND restore_source_archive_sha256 IS NOT NULL
            AND restore_source_runtime_revision_sha256 IS NOT NULL
            AND restore_source_immutable_input_sha256 IS NOT NULL
            AND restore_source_proof_sha256 IS NOT NULL
            AND restore_source_version > 0
            AND restore_source_archive_object_key IS NOT NULL
            AND restore_source_archive_version_id IS NOT NULL
            AND restore_source_archive_kms_key_arn IS NOT NULL
            AND restore_source_archive_object_lock_mode = 'COMPLIANCE'
            AND restore_source_archive_retain_until IS NOT NULL
            AND restore_source_retention_policy_id IS NOT NULL
            AND restore_source_retention_policy_version > 0
            AND restore_source_provenance_sha256 IS NOT NULL)
    );

ALTER TABLE control_plane.resource_retention_policies
    ADD COLUMN actor_id uuid,
    ADD COLUMN reason_code text,
    ADD COLUMN idempotency_key_sha256 text,
    ADD COLUMN request_sha256 text,
    ADD COLUMN supersedes_version bigint,
    ADD CONSTRAINT resource_retention_policies_lifecycle_v2_ck CHECK (
        (version = 1 AND supersedes_version IS NULL)
        OR (version > 1 AND supersedes_version = version - 1)
    ),
    ADD CONSTRAINT resource_retention_policies_receipt_v2_ck CHECK (
        (actor_id IS NULL AND reason_code IS NULL
            AND idempotency_key_sha256 IS NULL AND request_sha256 IS NULL)
        OR (actor_id IS NOT NULL
            AND reason_code ~ '^[a-z][a-z0-9._-]{1,95}$'
            AND idempotency_key_sha256 ~ '^[a-f0-9]{64}$'
            AND request_sha256 ~ '^[a-f0-9]{64}$')
    );
CREATE UNIQUE INDEX resource_retention_policies_idempotency_uidx
    ON control_plane.resource_retention_policies (
        organization_id, project_id, idempotency_key_sha256
    ) WHERE idempotency_key_sha256 IS NOT NULL;

-- Новый project scope получает approved default в той же owner transaction,
-- которая создаёт aggregate. Caller не выбирает policy/scope.
-- +goose StatementBegin
CREATE FUNCTION control_plane.ensure_default_resource_retention_policy()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF NEW.kind = 'PROJECT' THEN
        INSERT INTO control_plane.resource_retention_policies (
            organization_id, project_id, policy_id, version,
            pvc_retention_seconds, archive_retention_seconds,
            effective_at, created_at
        ) VALUES (
            NEW.organization_id, NEW.project_id, 'prototype-testing-v1', 1,
            604800, 7776000, NEW.created_at, NEW.created_at
        ) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER resources_default_retention_policy
AFTER INSERT ON control_plane.resources
FOR EACH ROW EXECUTE FUNCTION control_plane.ensure_default_resource_retention_policy();

CREATE TABLE control_plane.runtime_retention_holds (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    hold_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('MANUAL', 'LEGAL')),
    state text NOT NULL CHECK (state IN ('ACTIVE', 'RELEASED')),
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    actor_id uuid NOT NULL,
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9._-]{1,95}$'),
    idempotency_key_sha256 text NOT NULL CHECK (idempotency_key_sha256 ~ '^[a-f0-9]{64}$'),
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    released_at timestamptz,
    PRIMARY KEY (organization_id, project_id, hold_id),
    UNIQUE (organization_id, project_id, idempotency_key_sha256),
    CHECK ((state = 'ACTIVE') = (released_at IS NULL))
);
CREATE UNIQUE INDEX runtime_retention_holds_active_kind_uidx
    ON control_plane.runtime_retention_holds (
        organization_id, project_id, session_id, kind
    ) WHERE state = 'ACTIVE';
ALTER TABLE control_plane.runtime_retention_holds ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_retention_holds FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_retention_holds_runtime_scope
    ON control_plane.runtime_retention_holds
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
REVOKE ALL ON control_plane.runtime_retention_holds FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON control_plane.runtime_retention_holds TO control_plane_runtime;
GRANT INSERT, UPDATE ON control_plane.resource_retention_policies TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260804000300, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260804000300 is forward-only: runtime residual owner state cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
