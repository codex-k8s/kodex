-- +goose Up
-- Restore operation закрепляет выбранный backup и новую attempt в owner state;
-- browser не получает storage locator, PVC tuple или worker grant.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.runtime_executions
    ADD COLUMN restore_source_fence bigint
        CHECK (restore_source_fence BETWEEN 1 AND 9007199254740991),
    ADD COLUMN restore_source_proof_reference text;

ALTER TABLE control_plane.runtime_executions
    DROP CONSTRAINT runtime_executions_restore_source_v4_ck,
    ADD CONSTRAINT runtime_executions_restore_source_v5_ck CHECK (
        (restore_source_execution_id IS NULL AND restore_source_archive_reference IS NULL
            AND restore_source_archive_sha256 IS NULL
            AND restore_source_runtime_revision_sha256 IS NULL
            AND restore_source_immutable_input_sha256 IS NULL
            AND restore_source_proof_reference IS NULL
            AND restore_source_proof_sha256 IS NULL
            AND restore_source_version IS NULL AND restore_source_fence IS NULL
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
            AND restore_source_proof_reference IS NOT NULL
            AND restore_source_proof_sha256 IS NOT NULL
            AND restore_source_version > 0 AND restore_source_fence > 0
            AND restore_source_archive_object_key IS NOT NULL
            AND restore_source_archive_version_id IS NOT NULL
            AND restore_source_archive_kms_key_arn IS NOT NULL
            AND restore_source_archive_object_lock_mode = 'COMPLIANCE'
            AND restore_source_archive_retain_until IS NOT NULL
            AND restore_source_retention_policy_id IS NOT NULL
            AND restore_source_retention_policy_version > 0
            AND restore_source_provenance_sha256 IS NOT NULL)
    );

CREATE TABLE control_plane.runtime_restore_operations (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    owner_actor_id uuid NOT NULL,
    backup_execution_id uuid NOT NULL,
    source_version bigint NOT NULL CHECK (source_version BETWEEN 1 AND 9007199254740991),
    source_fence bigint NOT NULL CHECK (source_fence BETWEEN 1 AND 9007199254740991),
    archive_sha256 text NOT NULL CHECK (archive_sha256 ~ '^[a-f0-9]{64}$'),
    provenance_sha256 text NOT NULL CHECK (provenance_sha256 ~ '^[a-f0-9]{64}$'),
    source_authority_sha256 text NOT NULL CHECK (source_authority_sha256 ~ '^[a-f0-9]{64}$'),
    session_id uuid NOT NULL,
    generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 100),
    consumed_generation bigint NOT NULL DEFAULT 0 CHECK (consumed_generation BETWEEN 0 AND generation),
    revoked_generation bigint NOT NULL DEFAULT 0 CHECK (revoked_generation BETWEEN 0 AND generation),
    target_turn_id uuid NOT NULL,
    target_attempt integer NOT NULL CHECK (target_attempt BETWEEN 1 AND 100),
    target_execution_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (organization_id, project_id, backup_execution_id),
    UNIQUE (organization_id, project_id, target_execution_id),
    FOREIGN KEY (organization_id, project_id, session_id)
        REFERENCES control_plane.resources (organization_id, project_id, id),
    FOREIGN KEY (organization_id, project_id, target_turn_id)
        REFERENCES control_plane.resources (organization_id, project_id, id),
    FOREIGN KEY (organization_id, project_id, backup_execution_id)
        REFERENCES control_plane.runtime_executions (organization_id, project_id, id)
);
CREATE INDEX runtime_restore_operations_owner_page_idx
    ON control_plane.runtime_restore_operations (
        organization_id, project_id, owner_actor_id, id
    );
ALTER TABLE control_plane.runtime_restore_operations OWNER TO control_plane_owner;
ALTER TABLE control_plane.runtime_restore_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_restore_operations FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_restore_operations_runtime_scope
    ON control_plane.runtime_restore_operations
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.runtime_restore_operations FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON control_plane.runtime_restore_operations TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260806000100, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260806000100 is forward-only: owner restore lineage cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
