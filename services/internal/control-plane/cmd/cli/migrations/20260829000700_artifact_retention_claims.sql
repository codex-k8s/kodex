-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.artifacts
    ADD COLUMN retention_claim_owner text,
    ADD COLUMN retention_claim_generation bigint NOT NULL DEFAULT 0
        CHECK (retention_claim_generation >= 0),
    ADD COLUMN retention_claim_expires_at timestamptz,
    ADD CONSTRAINT artifacts_retention_claim_consistency CHECK (
        (retention_claim_owner IS NULL AND retention_claim_expires_at IS NULL)
        OR (
            lifecycle_state = 'PURGE_PENDING'
            AND retention_claim_owner IS NOT NULL
            AND char_length(retention_claim_owner) BETWEEN 1 AND 128
            AND retention_claim_expires_at IS NOT NULL
        )
    );

CREATE INDEX artifacts_retention_claimable
    ON control_plane.artifacts (purge_after, retention_claim_expires_at, id)
    WHERE lifecycle_state IN ('DELETED', 'PURGE_PENDING');

GRANT CONNECT ON DATABASE control_plane TO artifact_retention_runtime_g1;
GRANT USAGE ON SCHEMA control_plane TO artifact_retention_runtime;
GRANT SELECT ON TABLE control_plane.artifacts, control_plane.artifact_content,
    control_plane.organizations, control_plane.projects, control_plane.subjects
    TO artifact_retention_runtime;
GRANT UPDATE (
    lifecycle_state, retention_claim_owner, retention_claim_generation,
    retention_claim_expires_at, version, file_name, media_type, size_bytes,
    digest, scan_state, preview_state, purged_at
) ON control_plane.artifacts TO artifact_retention_runtime;
GRANT DELETE ON TABLE control_plane.artifact_bindings,
    control_plane.artifact_download_grants, control_plane.artifact_content
    TO artifact_retention_runtime;
GRANT INSERT ON TABLE control_plane.subjects, control_plane.audit_events
    TO artifact_retention_runtime;
GRANT UPDATE (active, updated_at) ON control_plane.subjects
    TO artifact_retention_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
REVOKE ALL PRIVILEGES ON TABLE control_plane.artifacts,
    control_plane.artifact_content, control_plane.organizations,
    control_plane.projects, control_plane.subjects,
    control_plane.artifact_bindings, control_plane.artifact_download_grants,
    control_plane.audit_events FROM artifact_retention_runtime;
REVOKE USAGE ON SCHEMA control_plane FROM artifact_retention_runtime;
REVOKE CONNECT ON DATABASE control_plane FROM artifact_retention_runtime_g1;
DROP INDEX control_plane.artifacts_retention_claimable;
ALTER TABLE control_plane.artifacts
    DROP CONSTRAINT artifacts_retention_claim_consistency,
    DROP COLUMN retention_claim_expires_at,
    DROP COLUMN retention_claim_generation,
    DROP COLUMN retention_claim_owner;
RESET ROLE;
