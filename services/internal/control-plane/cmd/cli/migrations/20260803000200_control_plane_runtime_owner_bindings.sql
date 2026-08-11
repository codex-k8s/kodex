-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Вторая review-волна закрепляет bot ownership, versioned retention,
-- одноразовый restore и immutable workload/credential tickets в owner row.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.runtime_agent_bindings (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    turn_id uuid NOT NULL REFERENCES control_plane.resources (id),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[a-f0-9]{64}$'),
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.resources (id),
    runtime_revision_version bigint NOT NULL CHECK (runtime_revision_version > 0),
    runtime_revision_sha256 text NOT NULL CHECK (runtime_revision_sha256 ~ '^[a-f0-9]{64}$'),
    agent_session_key text NOT NULL CHECK (length(agent_session_key) BETWEEN 1 AND 256),
    agent_session_id bigint NOT NULL CHECK (agent_session_id > 0),
    agent_session_version bigint NOT NULL CHECK (agent_session_version > 0),
    agent_session_binding_sha256 text NOT NULL CHECK (agent_session_binding_sha256 ~ '^[a-f0-9]{64}$'),
    agent_session_turn_id bigint NOT NULL CHECK (agent_session_turn_id > 0),
    agent_run_id text NOT NULL CHECK (length(agent_run_id) BETWEEN 1 AND 256),
    agent_session_turn_version bigint NOT NULL CHECK (agent_session_turn_version > 0),
    agent_turn_binding_sha256 text NOT NULL CHECK (agent_turn_binding_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, turn_id, attempt),
    UNIQUE (organization_id, project_id, agent_session_id, agent_session_turn_id, agent_run_id, attempt)
);

ALTER TABLE control_plane.runtime_agent_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_agent_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_agent_bindings_runtime_scope
    ON control_plane.runtime_agent_bindings
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
REVOKE ALL ON control_plane.runtime_agent_bindings FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.runtime_agent_bindings TO control_plane_runtime;

CREATE TABLE control_plane.resource_retention_policies (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    policy_id text NOT NULL CHECK (policy_id ~ '^[a-z][a-z0-9._-]{0,95}$'),
    version bigint NOT NULL CHECK (version > 0),
    pvc_retention_seconds bigint NOT NULL CHECK (pvc_retention_seconds BETWEEN 86400 AND 2592000),
    archive_retention_seconds bigint NOT NULL CHECK (archive_retention_seconds BETWEEN 7776000 AND 315360000),
    effective_at timestamptz NOT NULL,
    retired_at timestamptz,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, policy_id, version),
    CHECK (retired_at IS NULL OR retired_at > effective_at)
);
CREATE UNIQUE INDEX resource_retention_policies_current_uidx
    ON control_plane.resource_retention_policies (organization_id, project_id)
    WHERE retired_at IS NULL;
ALTER TABLE control_plane.resource_retention_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.resource_retention_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY resource_retention_policies_runtime_scope
    ON control_plane.resource_retention_policies
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
REVOKE ALL ON control_plane.resource_retention_policies FROM PUBLIC;
GRANT SELECT ON control_plane.resource_retention_policies TO control_plane_runtime;

INSERT INTO control_plane.resource_retention_policies (
    organization_id, project_id, policy_id, version,
    pvc_retention_seconds, archive_retention_seconds,
    effective_at, created_at
)
SELECT DISTINCT organization_id, project_id, 'prototype-testing-v1', 1,
       604800, 7776000, clock_timestamp(), clock_timestamp()
FROM control_plane.resources
ON CONFLICT DO NOTHING;

ALTER TABLE control_plane.runtime_executions
    ADD COLUMN effective_runtime_sha256 text
        CHECK (effective_runtime_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN agent_session_key text
        CHECK (length(agent_session_key) BETWEEN 1 AND 256),
    ADD COLUMN agent_session_id bigint CHECK (agent_session_id > 0),
    ADD COLUMN agent_session_turn_id bigint CHECK (agent_session_turn_id > 0),
    ADD COLUMN agent_run_id text CHECK (length(agent_run_id) BETWEEN 1 AND 256),
    ADD COLUMN agent_binding_sha256 text
        CHECK (agent_binding_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN retention_policy_id text
        CHECK (retention_policy_id ~ '^[a-z][a-z0-9._-]{0,95}$'),
    ADD COLUMN retention_policy_version bigint CHECK (retention_policy_version > 0),
    ADD COLUMN pvc_retention_seconds bigint
        CHECK (pvc_retention_seconds BETWEEN 86400 AND 2592000),
    ADD COLUMN archive_retention_seconds bigint
        CHECK (archive_retention_seconds BETWEEN 7776000 AND 315360000),
    ADD COLUMN archive_retain_until timestamptz,
    ADD COLUMN archive_object_key text,
    ADD COLUMN archive_version_id text,
    ADD COLUMN archive_kms_key_arn text,
    ADD COLUMN archive_object_lock_mode text,
    ADD COLUMN archive_provenance_sha256 text CHECK (
        archive_provenance_sha256 IS NULL OR archive_provenance_sha256 ~ '^[a-f0-9]{64}$'
    ),
    ADD COLUMN pvc_cleanup_eligible_at timestamptz,
    ADD COLUMN capacity_observation_expires_at timestamptz,
    ADD COLUMN reschedule_after timestamptz,
    ADD COLUMN restore_assignment_state text NOT NULL DEFAULT 'NONE'
        CHECK (restore_assignment_state IN ('NONE', 'ASSIGNED', 'BOUND', 'CONSUMED')),
    ADD COLUMN restore_assignment_generation bigint NOT NULL DEFAULT 0
        CHECK (restore_assignment_generation BETWEEN 0 AND 9007199254740991),
    ADD COLUMN restore_target_pvc_name text,
    ADD COLUMN restore_target_pvc_uid uuid,
    ADD COLUMN restore_target_pvc_resource_version text,
    ADD COLUMN rehydrate_proof_reference text,
    ADD COLUMN rehydrate_proof_sha256 text
        CHECK (rehydrate_proof_sha256 IS NULL OR rehydrate_proof_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN credential_snapshot_sha256 text
        CHECK (credential_snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN workload_ticket_sha256 text
        CHECK (workload_ticket_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN restore_source_execution_version bigint,
    ADD COLUMN restore_source_archive_key text,
    ADD COLUMN restore_source_archive_version_id text,
    ADD COLUMN restore_source_kms_key_arn text,
    ADD COLUMN restore_source_object_lock_mode text,
    ADD COLUMN restore_source_archive_retain_until timestamptz,
    ADD COLUMN restore_source_retention_policy_id text,
    ADD COLUMN restore_source_retention_policy_version bigint,
    ADD COLUMN restore_source_provenance_sha256 text CHECK (
        restore_source_provenance_sha256 IS NULL OR restore_source_provenance_sha256 ~ '^[a-f0-9]{64}$'
    ),
    ADD CONSTRAINT runtime_executions_restore_assignment_ck CHECK (
        (restore_assignment_state = 'NONE'
            AND restore_assignment_generation = 0
            AND restore_source_execution_id IS NULL
            AND restore_target_pvc_name IS NULL AND restore_target_pvc_uid IS NULL
            AND restore_target_pvc_resource_version IS NULL
            AND rehydrate_proof_reference IS NULL AND rehydrate_proof_sha256 IS NULL)
        OR (restore_assignment_state = 'ASSIGNED'
            AND restore_assignment_generation > 0
            AND restore_source_execution_id IS NOT NULL
            AND restore_target_pvc_name IS NULL AND restore_target_pvc_uid IS NULL
            AND restore_target_pvc_resource_version IS NULL
            AND rehydrate_proof_reference IS NULL AND rehydrate_proof_sha256 IS NULL)
        OR (restore_assignment_state = 'BOUND'
            AND restore_assignment_generation > 0
            AND restore_source_execution_id IS NOT NULL
            AND restore_target_pvc_name IS NOT NULL AND restore_target_pvc_uid IS NOT NULL
            AND restore_target_pvc_resource_version IS NOT NULL
            AND rehydrate_proof_reference IS NULL AND rehydrate_proof_sha256 IS NULL)
        OR (restore_assignment_state = 'CONSUMED'
            AND restore_assignment_generation > 0
            AND restore_source_execution_id IS NOT NULL
            AND restore_target_pvc_name IS NOT NULL AND restore_target_pvc_uid IS NOT NULL
            AND restore_target_pvc_resource_version IS NOT NULL
            AND rehydrate_proof_reference IS NOT NULL AND rehydrate_proof_sha256 IS NOT NULL)
    );

UPDATE control_plane.runtime_executions AS execution
SET effective_runtime_sha256 = COALESCE(
        NULLIF(revision.spec ->> 'effectiveRuntimeSha256', ''),
        execution.runtime_revision_sha256
    ),
    agent_session_key = 'legacy-upgrade:' || execution.session_id::text,
    agent_session_id = execution.attempt,
    agent_session_turn_id = execution.attempt,
    agent_run_id = 'legacy-upgrade:' || execution.turn_id::text,
    agent_binding_sha256 = execution.immutable_input_sha256,
    retention_policy_id = policy.policy_id,
    retention_policy_version = policy.version,
    pvc_retention_seconds = policy.pvc_retention_seconds,
    archive_retention_seconds = policy.archive_retention_seconds,
    archive_retain_until = execution.updated_at
        + make_interval(secs => policy.archive_retention_seconds + 86400),
    pvc_cleanup_eligible_at = execution.updated_at
        + make_interval(secs => policy.pvc_retention_seconds),
    capacity_observation_expires_at = execution.updated_at,
    reschedule_after = execution.updated_at,
    credential_snapshot_sha256 = execution.runtime_revision_sha256,
    workload_ticket_sha256 = execution.immutable_input_sha256
FROM control_plane.resources AS revision,
     control_plane.resource_retention_policies AS policy
WHERE revision.id = execution.runtime_revision_id
  AND policy.organization_id = execution.organization_id
  AND policy.project_id = execution.project_id
  AND policy.retired_at IS NULL;

ALTER TABLE control_plane.runtime_executions
    ALTER COLUMN effective_runtime_sha256 SET NOT NULL,
    ALTER COLUMN agent_session_key SET NOT NULL,
    ALTER COLUMN agent_session_id SET NOT NULL,
    ALTER COLUMN agent_session_turn_id SET NOT NULL,
    ALTER COLUMN agent_run_id SET NOT NULL,
    ALTER COLUMN agent_binding_sha256 SET NOT NULL,
    ALTER COLUMN retention_policy_id SET NOT NULL,
    ALTER COLUMN retention_policy_version SET NOT NULL,
    ALTER COLUMN pvc_retention_seconds SET NOT NULL,
    ALTER COLUMN archive_retention_seconds SET NOT NULL,
    ALTER COLUMN pvc_cleanup_eligible_at SET NOT NULL,
    ALTER COLUMN capacity_observation_expires_at SET NOT NULL,
    ALTER COLUMN reschedule_after SET NOT NULL,
    ALTER COLUMN credential_snapshot_sha256 SET NOT NULL,
    ALTER COLUMN workload_ticket_sha256 SET NOT NULL;

ALTER TABLE control_plane.runtime_executions
    ADD CONSTRAINT runtime_executions_archive_evidence_ck CHECK (
        (archive_object_key IS NULL AND archive_version_id IS NULL
            AND archive_kms_key_arn IS NULL AND archive_object_lock_mode IS NULL
            AND archive_provenance_sha256 IS NULL)
        OR (archive_reference IS NOT NULL AND archive_object_key IS NOT NULL
            AND archive_version_id IS NOT NULL AND archive_kms_key_arn IS NOT NULL
            AND archive_object_lock_mode = 'COMPLIANCE'
            AND archive_provenance_sha256 IS NOT NULL)
    );

ALTER TABLE control_plane.runtime_executions
    DROP CONSTRAINT runtime_executions_restore_source_v2_ck,
    ADD CONSTRAINT runtime_executions_restore_source_v3_ck CHECK (
        (restore_source_execution_id IS NULL AND restore_source_archive_reference IS NULL
            AND restore_source_archive_sha256 IS NULL
            AND restore_source_runtime_revision_sha256 IS NULL
            AND restore_source_immutable_input_sha256 IS NULL
            AND restore_source_proof_sha256 IS NULL
            AND restore_source_execution_version IS NULL
            AND restore_source_archive_key IS NULL AND restore_source_archive_version_id IS NULL
            AND restore_source_kms_key_arn IS NULL AND restore_source_object_lock_mode IS NULL
            AND restore_source_archive_retain_until IS NULL
            AND restore_source_retention_policy_id IS NULL
            AND restore_source_retention_policy_version IS NULL
            AND restore_source_provenance_sha256 IS NULL)
        OR (restore_source_execution_id IS NOT NULL AND restore_source_archive_reference IS NOT NULL
            AND restore_source_archive_sha256 IS NOT NULL
            AND restore_source_runtime_revision_sha256 IS NOT NULL
            AND restore_source_immutable_input_sha256 IS NOT NULL
            AND restore_source_proof_sha256 IS NOT NULL
            AND restore_source_execution_version > 0
            AND restore_source_archive_key IS NOT NULL AND restore_source_archive_version_id IS NOT NULL
            AND restore_source_kms_key_arn IS NOT NULL
            AND restore_source_object_lock_mode = 'COMPLIANCE'
            AND restore_source_archive_retain_until IS NOT NULL
            AND restore_source_retention_policy_id IS NOT NULL
            AND restore_source_retention_policy_version > 0
            AND restore_source_provenance_sha256 IS NOT NULL)
    );

ALTER TABLE control_plane.runtime_executions
    ALTER COLUMN restore_assignment_state DROP DEFAULT;

CREATE UNIQUE INDEX runtime_executions_restore_source_once_uidx
    ON control_plane.runtime_executions (organization_id, project_id, restore_source_execution_id)
    WHERE restore_source_execution_id IS NOT NULL
      AND (restore_assignment_state = 'CONSUMED'
           OR state NOT IN ('RETRIED', 'CANCELLED', 'EXPIRED'));

CREATE INDEX runtime_executions_pending_reschedule_idx
    ON control_plane.runtime_executions (
        organization_id, project_id, state, reschedule_after, capacity_observation_expires_at, id
    )
    WHERE state = 'PENDING';

UPDATE control_plane.schema_state
SET version = 20260803000200, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260803000200 is forward-only: runtime owner bindings cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
