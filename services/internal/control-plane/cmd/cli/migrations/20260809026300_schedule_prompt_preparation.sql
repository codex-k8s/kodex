-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Durable single-winner внешнего Schedule prompt effect. Строка связывает
-- exact owner/target/preconditions/idempotency/content до S3 RPC; private
-- object locator никогда не выходит из control-plane.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.schedule_prompt_preparations (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    owner_actor_id uuid NOT NULL,
    key_hash text NOT NULL CHECK (key_hash ~ '^[a-f0-9]{64}$'),
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
    semantic_sha256 text NOT NULL CHECK (semantic_sha256 ~ '^[a-f0-9]{64}$'),
    action text NOT NULL CHECK (action IN ('create', 'update')),
    target_id uuid,
    expected_version bigint NOT NULL CHECK (
        expected_version BETWEEN 0 AND 9007199254740991
    ),
    object_key text NOT NULL CHECK (
        length(object_key) BETWEEN 1 AND 1024
        AND object_key = btrim(object_key)
    ),
    state text NOT NULL CHECK (
        state IN ('WRITING', 'READY', 'AMBIGUOUS', 'CONSUMED')
    ),
    generation bigint NOT NULL CHECK (
        generation BETWEEN 1 AND 9007199254740991
    ),
    lease_expires_at timestamptz,
    object_reference text,
    object_version_id text,
    object_sha256 text,
    object_size bigint CHECK (object_size BETWEEN 1 AND 262144),
    object_media_type text,
    result_schedule_id uuid,
    result_schedule_version bigint CHECK (
        result_schedule_version BETWEEN 1 AND 9007199254740991
    ),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, owner_actor_id, key_hash),
    CHECK ((action = 'create') = (expected_version = 0)),
    CHECK ((action = 'update') = (target_id IS NOT NULL)),
    CHECK ((state = 'WRITING') = (lease_expires_at IS NOT NULL)),
    CHECK (
        (state IN ('READY', 'CONSUMED')) =
        (
            object_reference IS NOT NULL
            AND object_version_id IS NOT NULL
            AND object_sha256 ~ '^[a-f0-9]{64}$'
            AND object_size IS NOT NULL
            AND object_media_type = 'text/markdown'
        )
    ),
    CHECK (
        (state = 'CONSUMED') =
        (result_schedule_id IS NOT NULL AND result_schedule_version IS NOT NULL)
    )
);
CREATE INDEX schedule_prompt_preparations_recovery_idx
    ON control_plane.schedule_prompt_preparations (
        state, lease_expires_at, updated_at, organization_id, project_id
    );
ALTER TABLE control_plane.schedule_prompt_preparations
    OWNER TO control_plane_owner;
ALTER TABLE control_plane.schedule_prompt_preparations
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.schedule_prompt_preparations
    FORCE ROW LEVEL SECURITY;
CREATE POLICY schedule_prompt_preparations_runtime_scope
    ON control_plane.schedule_prompt_preparations
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
REVOKE ALL ON control_plane.schedule_prompt_preparations FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.schedule_prompt_preparations
    TO control_plane_runtime;
GRANT UPDATE (
    state, generation, lease_expires_at, object_reference,
    object_version_id, object_sha256, object_size, object_media_type,
    result_schedule_id, result_schedule_version, updated_at
) ON control_plane.schedule_prompt_preparations TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260809026300, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'migration 20260809026300 is forward-only: durable Schedule prompt preparations cannot be discarded';
END $$;
-- +goose StatementEnd
