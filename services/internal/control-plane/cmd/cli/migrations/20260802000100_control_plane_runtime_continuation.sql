-- +goose Up
-- Immutable runtime snapshot/fence и typed integration continuation являются
-- owner state control-plane и не используют generic resources lifecycle.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.runtime_executions (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    process_id uuid NOT NULL REFERENCES control_plane.resources (id),
    session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    thread_id text NOT NULL CHECK (length(thread_id) BETWEEN 1 AND 256),
    role_id uuid NOT NULL REFERENCES control_plane.resources (id),
    turn_id uuid NOT NULL REFERENCES control_plane.resources (id),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.resources (id),
    runtime_revision_version bigint NOT NULL CHECK (runtime_revision_version > 0),
    runtime_revision_sha256 text NOT NULL CHECK (runtime_revision_sha256 ~ '^[a-f0-9]{64}$'),
    immutable_input_sha256 text NOT NULL CHECK (immutable_input_sha256 ~ '^[a-f0-9]{64}$'),
    resource_class text NOT NULL CHECK (resource_class IN ('STANDARD', 'HIGH_MEMORY', 'ACCELERATED')),
    cluster_access_profile text NOT NULL CHECK (cluster_access_profile IN (
        'NONE', 'PROJECT_READ_ONLY', 'PROJECT_WORKLOAD_OPERATOR'
    )),
    workload_id text NOT NULL CHECK (workload_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    workload_spiffe_id text NOT NULL CHECK (workload_spiffe_id LIKE 'spiffe://%'),
    grant_generation bigint NOT NULL CHECK (grant_generation > 0),
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    fence bigint NOT NULL CHECK (fence BETWEEN 1 AND 9007199254740991),
    state text NOT NULL CHECK (state IN (
        'PENDING', 'ADMITTED', 'RUNNING', 'SUCCEEDED', 'FAILED',
        'CANCELLED', 'EXPIRED', 'RETRIED'
    )),
    lease_id uuid,
    lease_token_sha256 text CHECK (lease_token_sha256 IS NULL OR lease_token_sha256 ~ '^[a-f0-9]{64}$'),
    lease_expires_at timestamptz,
    terminal_outcome text CHECK (terminal_outcome IS NULL OR terminal_outcome IN ('SUCCEEDED', 'FAILED')),
    terminal_reference text,
    terminal_sha256 text CHECK (terminal_sha256 IS NULL OR terminal_sha256 ~ '^[a-f0-9]{64}$'),
    archive_reference text,
    archive_sha256 text CHECK (archive_sha256 IS NULL OR archive_sha256 ~ '^[a-f0-9]{64}$'),
    restore_proof_reference text,
    restore_proof_sha256 text CHECK (restore_proof_sha256 IS NULL OR restore_proof_sha256 ~ '^[a-f0-9]{64}$'),
    restore_verifier_workload_id text,
    cleanup_authorization_id uuid,
    cleanup_authorization_expires_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (organization_id, project_id, id),
    UNIQUE (organization_id, project_id, turn_id, attempt),
    CHECK (
        (lease_id IS NULL AND lease_token_sha256 IS NULL AND lease_expires_at IS NULL)
        OR (lease_id IS NOT NULL AND lease_token_sha256 IS NOT NULL AND lease_expires_at IS NOT NULL)
    ),
    CHECK (
        (state IN ('ADMITTED', 'RUNNING')) =
        (lease_id IS NOT NULL AND lease_token_sha256 IS NOT NULL AND lease_expires_at IS NOT NULL)
    ),
    CHECK (
        (terminal_outcome IS NULL AND terminal_reference IS NULL AND terminal_sha256 IS NULL)
        OR (terminal_outcome IS NOT NULL AND terminal_reference IS NOT NULL AND terminal_sha256 IS NOT NULL)
    ),
    CHECK (
        (state = 'SUCCEEDED' AND terminal_outcome = 'SUCCEEDED')
        OR (state = 'FAILED' AND terminal_outcome = 'FAILED')
        OR (state NOT IN ('SUCCEEDED', 'FAILED') AND terminal_outcome IS NULL)
    ),
    CHECK (
        (archive_reference IS NULL AND archive_sha256 IS NULL)
        OR (archive_reference IS NOT NULL AND archive_sha256 IS NOT NULL)
    ),
    CHECK (
        (restore_proof_reference IS NULL AND restore_proof_sha256 IS NULL AND restore_verifier_workload_id IS NULL)
        OR (restore_proof_reference IS NOT NULL AND restore_proof_sha256 IS NOT NULL AND restore_verifier_workload_id IS NOT NULL)
    ),
    CHECK (archive_sha256 IS NULL OR state IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'RETRIED')),
    CHECK (restore_proof_sha256 IS NULL OR archive_sha256 IS NOT NULL),
    CHECK (
        (cleanup_authorization_id IS NULL AND cleanup_authorization_expires_at IS NULL)
        OR (cleanup_authorization_id IS NOT NULL
            AND cleanup_authorization_expires_at > updated_at
            AND archive_sha256 IS NOT NULL AND restore_proof_sha256 IS NOT NULL)
    )
);
CREATE INDEX runtime_executions_expiry_idx
    ON control_plane.runtime_executions (
        organization_id, project_id, turn_id, attempt, lease_expires_at, id
    )
    WHERE state IN ('ADMITTED', 'RUNNING');

CREATE TABLE control_plane.runtime_execution_incidents (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    execution_fence bigint NOT NULL CHECK (execution_fence > 0),
    kind text NOT NULL CHECK (kind IN ('HEARTBEAT_MISSED', 'RECONCILE_FAILED', 'WORKLOAD_UNAVAILABLE')),
    evidence_sha256 text NOT NULL CHECK (evidence_sha256 ~ '^[a-f0-9]{64}$'),
    workload_id text NOT NULL CHECK (workload_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    occurred_at timestamptz NOT NULL,
    UNIQUE (organization_id, project_id, execution_id, id),
    FOREIGN KEY (organization_id, project_id, execution_id)
        REFERENCES control_plane.runtime_executions (organization_id, project_id, id)
);

CREATE TABLE control_plane.integration_continuations (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    process_id uuid NOT NULL REFERENCES control_plane.resources (id),
    session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    session_version bigint NOT NULL CHECK (session_version > 0),
    thread_id text NOT NULL CHECK (length(thread_id) BETWEEN 1 AND 256),
    role_id uuid NOT NULL REFERENCES control_plane.resources (id),
    turn_id uuid NOT NULL REFERENCES control_plane.resources (id),
    turn_version bigint NOT NULL CHECK (turn_version > 0),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.resources (id),
    runtime_revision_version bigint NOT NULL CHECK (runtime_revision_version > 0),
    runtime_revision_sha256 text NOT NULL CHECK (runtime_revision_sha256 ~ '^[a-f0-9]{64}$'),
    immutable_input_sha256 text NOT NULL CHECK (immutable_input_sha256 ~ '^[a-f0-9]{64}$'),
    grant_generation bigint NOT NULL CHECK (grant_generation > 0),
    invocation_id uuid NOT NULL,
    approval_id uuid NOT NULL,
    integration_id uuid NOT NULL REFERENCES control_plane.resources (id),
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
    approval_state text NOT NULL CHECK (approval_state IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'CANCELLED')),
    execution_state text NOT NULL CHECK (execution_state IN ('NOT_STARTED', 'EXECUTING', 'SUCCEEDED', 'FAILED', 'NOT_APPLICABLE')),
    continuation_state text NOT NULL CHECK (continuation_state IN ('SUSPENDED', 'READY', 'REJOINED')),
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    fence bigint NOT NULL CHECK (fence BETWEEN 1 AND 9007199254740991),
    approval_expires_at timestamptz NOT NULL,
    decision_reference text,
    decision_sha256 text CHECK (decision_sha256 IS NULL OR decision_sha256 ~ '^[a-f0-9]{64}$'),
    result_reference text,
    result_sha256 text CHECK (result_sha256 IS NULL OR result_sha256 ~ '^[a-f0-9]{64}$'),
    error_code text,
    error_reference text,
    error_sha256 text CHECK (error_sha256 IS NULL OR error_sha256 ~ '^[a-f0-9]{64}$'),
    continuation_turn_id uuid REFERENCES control_plane.resources (id),
    continuation_turn_version bigint,
    continuation_runtime_revision_id uuid REFERENCES control_plane.resources (id),
    continuation_runtime_revision_version bigint,
    continuation_input_sha256 text CHECK (continuation_input_sha256 IS NULL OR continuation_input_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (organization_id, project_id, invocation_id),
    UNIQUE (organization_id, project_id, turn_id, attempt, invocation_id),
    CHECK (
        (decision_reference IS NULL AND decision_sha256 IS NULL)
        OR (decision_reference IS NOT NULL AND decision_sha256 IS NOT NULL)
    ),
    CHECK (
        (result_reference IS NULL AND result_sha256 IS NULL)
        OR (result_reference IS NOT NULL AND result_sha256 IS NOT NULL)
    ),
    CHECK (
        (error_code IS NULL AND error_reference IS NULL AND error_sha256 IS NULL)
        OR (error_code IS NOT NULL AND error_reference IS NOT NULL AND error_sha256 IS NOT NULL)
    ),
    CHECK (
        (continuation_turn_id IS NULL AND continuation_turn_version IS NULL
         AND continuation_runtime_revision_id IS NULL
         AND continuation_runtime_revision_version IS NULL
         AND continuation_input_sha256 IS NULL)
        OR (continuation_turn_id IS NOT NULL AND continuation_turn_version > 0
            AND continuation_runtime_revision_id IS NOT NULL
            AND continuation_runtime_revision_version > 0
            AND continuation_input_sha256 IS NOT NULL)
    ),
    CHECK (
        (approval_state = 'PENDING' AND decision_reference IS NULL
         AND execution_state = 'NOT_STARTED' AND continuation_state = 'SUSPENDED')
        OR (approval_state = 'APPROVED' AND decision_reference IS NOT NULL
            AND execution_state IN ('NOT_STARTED', 'EXECUTING')
            AND continuation_state = 'SUSPENDED')
        OR (approval_state = 'APPROVED' AND execution_state IN ('SUCCEEDED', 'FAILED')
            AND continuation_state IN ('READY', 'REJOINED'))
        OR (approval_state IN ('REJECTED', 'EXPIRED', 'CANCELLED')
            AND decision_reference IS NOT NULL AND execution_state = 'NOT_APPLICABLE'
            AND continuation_state IN ('READY', 'REJOINED'))
    ),
    CHECK ((execution_state = 'SUCCEEDED') = (result_reference IS NOT NULL)),
    CHECK ((execution_state = 'FAILED') = (error_code IS NOT NULL)),
    CHECK (
        (continuation_state = 'SUSPENDED' AND continuation_turn_id IS NULL)
        OR (continuation_state IN ('READY', 'REJOINED') AND continuation_turn_id IS NOT NULL)
    )
);
CREATE INDEX integration_continuations_expiry_idx
    ON control_plane.integration_continuations (
        organization_id, project_id, turn_id, attempt, approval_expires_at, id
    ) WHERE approval_state = 'PENDING';
CREATE UNIQUE INDEX integration_continuations_rejoin_turn_idx
    ON control_plane.integration_continuations (continuation_turn_id)
    WHERE continuation_turn_id IS NOT NULL;

ALTER TABLE control_plane.runtime_executions ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_executions FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_execution_incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_execution_incidents FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.integration_continuations ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.integration_continuations FORCE ROW LEVEL SECURITY;

CREATE POLICY runtime_executions_runtime_scope
    ON control_plane.runtime_executions
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
CREATE POLICY runtime_incidents_runtime_scope
    ON control_plane.runtime_execution_incidents
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
CREATE POLICY integration_continuations_runtime_scope
    ON control_plane.integration_continuations
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );

REVOKE ALL ON control_plane.runtime_executions,
    control_plane.runtime_execution_incidents,
    control_plane.integration_continuations FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON control_plane.runtime_executions TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.runtime_execution_incidents TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.integration_continuations TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260802000100, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260802000100 is forward-only: runtime fences and continuation receipts cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
