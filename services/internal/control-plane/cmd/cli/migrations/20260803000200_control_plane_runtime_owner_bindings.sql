-- +goose Up
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
    UNIQUE (organization_id, project_id, agent_session_id, agent_session_turn_id, agent_run_id)
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

ALTER TABLE control_plane.runtime_executions
    ADD COLUMN effective_runtime_sha256 text NOT NULL
        CHECK (effective_runtime_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN agent_session_key text NOT NULL
        CHECK (length(agent_session_key) BETWEEN 1 AND 256),
    ADD COLUMN agent_session_id bigint NOT NULL CHECK (agent_session_id > 0),
    ADD COLUMN agent_session_turn_id bigint NOT NULL CHECK (agent_session_turn_id > 0),
    ADD COLUMN agent_run_id text NOT NULL CHECK (length(agent_run_id) BETWEEN 1 AND 256),
    ADD COLUMN agent_binding_sha256 text NOT NULL
        CHECK (agent_binding_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN retention_policy_id text NOT NULL
        CHECK (retention_policy_id ~ '^[a-z][a-z0-9._-]{0,95}$'),
    ADD COLUMN retention_policy_version bigint NOT NULL CHECK (retention_policy_version > 0),
    ADD COLUMN pvc_retention_seconds bigint NOT NULL
        CHECK (pvc_retention_seconds BETWEEN 86400 AND 2592000),
    ADD COLUMN archive_retention_seconds bigint NOT NULL
        CHECK (archive_retention_seconds BETWEEN 7776000 AND 315360000),
    ADD COLUMN archive_retain_until timestamptz,
    ADD COLUMN pvc_cleanup_eligible_at timestamptz NOT NULL,
    ADD COLUMN capacity_observation_expires_at timestamptz NOT NULL,
    ADD COLUMN reschedule_after timestamptz NOT NULL,
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
    ADD COLUMN credential_snapshot_sha256 text NOT NULL
        CHECK (credential_snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN workload_ticket_sha256 text NOT NULL
        CHECK (workload_ticket_sha256 ~ '^[a-f0-9]{64}$'),
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
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260803000200 is forward-only: runtime owner bindings cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
