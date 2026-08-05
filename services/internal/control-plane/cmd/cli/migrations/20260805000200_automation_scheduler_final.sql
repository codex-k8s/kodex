-- +goose Up
ALTER TABLE control_plane.schedule_occurrences
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN recovery_evidence_sha256 text,
    ADD COLUMN recovery_blocked_at timestamptz,
    DROP CONSTRAINT schedule_occurrences_state_check,
    DROP CONSTRAINT schedule_occurrence_lease_consistency,
    ADD CONSTRAINT schedule_occurrences_state_check CHECK (state IN (
        'QUEUED', 'RESERVED', 'CLAIMED', 'WAITING_OWNER', 'CONTINUATION',
        'RECOVERY_BLOCKED', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'SKIPPED', 'DEAD_LETTER'
    )),
    ADD CONSTRAINT schedule_occurrence_lease_consistency CHECK (
        (
            state IN ('RESERVED', 'CLAIMED')
            AND claimant_workload_id IS NOT NULL
            AND authority_generation IS NOT NULL
            AND token_hash ~ '^[a-f0-9]{64}$'
            AND claim_key_sha256 ~ '^[a-f0-9]{64}$'
            AND lease_expires_at IS NOT NULL
        )
        OR (
            state NOT IN ('RESERVED', 'CLAIMED')
            AND claimant_workload_id IS NULL
            AND authority_generation IS NULL
            AND token_hash IS NULL
            AND claim_key_sha256 IS NULL
            AND lease_expires_at IS NULL
        )
    ),
    ADD CONSTRAINT schedule_occurrence_recovery_evidence_complete CHECK (
        (state = 'RECOVERY_BLOCKED'
         AND recovery_evidence_sha256 ~ '^[a-f0-9]{64}$'
         AND recovery_blocked_at IS NOT NULL)
        OR
        (state <> 'RECOVERY_BLOCKED'
         AND recovery_evidence_sha256 IS NULL
         AND recovery_blocked_at IS NULL)
    );

CREATE TABLE control_plane.schedule_occurrence_capabilities (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    occurrence_id uuid NOT NULL,
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    immutable_input_sha256 text NOT NULL CHECK (immutable_input_sha256 ~ '^[a-f0-9]{64}$'),
    authority_generation bigint NOT NULL CHECK (authority_generation > 0),
    full_method text NOT NULL CHECK (full_method IN (
        '/controlplane.v1.ControlPlaneService/MaterializeScheduleOccurrence',
        '/controlplane.v1.ControlPlaneService/CompleteScheduleOccurrence'
    )),
    workload_id text NOT NULL CHECK (workload_id = 'automation-scheduler'),
    caller_spiffe_id text NOT NULL CHECK (
        caller_spiffe_id = 'spiffe://mattercodex.local/ns/mattercodex-system/sa/automation-scheduler'
    ),
    token_sha256 text NOT NULL UNIQUE CHECK (token_sha256 ~ '^[a-f0-9]{64}$'),
    state text NOT NULL CHECK (state IN ('ISSUED', 'CONSUMED', 'REVOKED')),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    UNIQUE (occurrence_id, attempt, full_method, authority_generation),
    FOREIGN KEY (organization_id, project_id, occurrence_id)
        REFERENCES control_plane.schedule_occurrences (organization_id, project_id, id),
    CHECK (expires_at > issued_at),
    CHECK (
        (state = 'ISSUED' AND consumed_at IS NULL AND revoked_at IS NULL)
        OR (state = 'CONSUMED' AND consumed_at IS NOT NULL AND revoked_at IS NULL)
        OR (state = 'REVOKED' AND consumed_at IS NULL AND revoked_at IS NOT NULL)
    )
);
CREATE INDEX schedule_occurrence_capabilities_current_idx
    ON control_plane.schedule_occurrence_capabilities (
        organization_id, project_id, occurrence_id, attempt, full_method, authority_generation
    );
ALTER TABLE control_plane.schedule_occurrence_capabilities OWNER TO control_plane_owner;
ALTER TABLE control_plane.schedule_occurrence_capabilities ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.schedule_occurrence_capabilities FORCE ROW LEVEL SECURITY;
CREATE POLICY schedule_occurrence_capabilities_scope
    ON control_plane.schedule_occurrence_capabilities
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
GRANT SELECT, INSERT, UPDATE ON control_plane.schedule_occurrence_capabilities TO control_plane_runtime;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260805000200 is forward-only: occurrence capabilities and recovery evidence cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
