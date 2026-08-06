-- +goose Up
-- Restore operation закрепляет выбранный backup и новую attempt в owner state;
-- browser не получает storage locator, PVC tuple или worker grant.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

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
    session_id uuid NOT NULL,
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
GRANT SELECT, INSERT ON control_plane.runtime_restore_operations TO control_plane_runtime;

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
