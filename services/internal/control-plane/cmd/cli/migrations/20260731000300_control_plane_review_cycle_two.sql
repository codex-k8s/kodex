-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Укрепление второго цикла добавляет локальные FTS и pgvector-проекцию,
-- полный жизненный цикл запусков расписаний и устойчивое к откату намерение.
-- PostgreSQL остаётся авторитетным источником; векторная проекция перестраиваема.
RESET ROLE;
CREATE EXTENSION IF NOT EXISTS vector;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

-- Локальный полнотекстовый индекс обслуживает title/content MEMORY_RECORD без
-- внешнего поставщика векторизации.
CREATE INDEX resources_memory_fts_idx
    ON control_plane.resources
    USING gin (
        to_tsvector(
            'simple',
            coalesce(spec ->> 'title', '') || ' ' || coalesce(spec ->> 'content', '')
        )
    )
    WHERE kind = 'MEMORY_RECORD' AND state = 'ACTIVE';

-- Сначала добавляются закреплённые поля исполнения и доставки, затем старые
-- строки приводятся к безопасному конечному состоянию перед ограничениями NOT NULL.
ALTER TABLE control_plane.schedule_occurrences
    ADD COLUMN prompt_profile_id uuid,
    ADD COLUMN prompt_revision bigint,
    ADD COLUMN runtime_revision_id uuid,
    ADD COLUMN session_policy text,
    ADD COLUMN room_id uuid,
    ADD COLUMN notification_policy text,
    ADD COLUMN maximum_execution_duration_ms bigint,
    ADD COLUMN coalesce boolean;

UPDATE control_plane.schedule_occurrences AS occurrence
SET
    prompt_profile_id = (schedule.spec ->> 'promptProfileId')::uuid,
    prompt_revision = (schedule.spec ->> 'promptRevision')::bigint,
    runtime_revision_id = (schedule.spec ->> 'runtimeRevisionId')::uuid,
    session_policy = schedule.spec ->> 'sessionPolicy',
    room_id = nullif(schedule.spec ->> 'roomId', '')::uuid,
    notification_policy = schedule.spec ->> 'notificationPolicy',
    maximum_execution_duration_ms =
        (schedule.spec ->> 'maximumExecutionDuration')::bigint / 1000000,
    coalesce = (schedule.spec ->> 'coalesce')::boolean
FROM control_plane.resources AS schedule
WHERE schedule.id = occurrence.schedule_id
  AND schedule.kind = 'SCHEDULE';

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM control_plane.schedule_occurrences
        WHERE prompt_profile_id IS NULL
           OR prompt_revision IS NULL
           OR runtime_revision_id IS NULL
           OR session_policy IS NULL
           OR notification_policy IS NULL
           OR maximum_execution_duration_ms IS NULL
           OR coalesce IS NULL
    ) THEN
        RAISE EXCEPTION
            'existing schedule occurrences cannot be upgraded without an exact schedule snapshot';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.schedule_occurrences
    ALTER COLUMN prompt_profile_id SET NOT NULL,
    ALTER COLUMN prompt_revision SET NOT NULL,
    ALTER COLUMN runtime_revision_id SET NOT NULL,
    ALTER COLUMN session_policy SET NOT NULL,
    ALTER COLUMN notification_policy SET NOT NULL,
    ALTER COLUMN maximum_execution_duration_ms SET NOT NULL,
    ALTER COLUMN coalesce SET NOT NULL,
    ADD CONSTRAINT schedule_occurrence_prompt_revision_positive
        CHECK (prompt_revision > 0),
    ADD CONSTRAINT schedule_occurrence_session_policy_closed
        CHECK (session_policy IN ('NEW', 'PERSISTENT', 'ROLLING')),
    ADD CONSTRAINT schedule_occurrence_notification_policy_closed
        CHECK (notification_policy IN (
            'ALWAYS', 'ON_ACTION', 'ON_FAILURE',
            'ON_ACTION_OR_FAILURE', 'AUDIT_ONLY'
        )),
    ADD CONSTRAINT schedule_occurrence_execution_duration_bounded
        CHECK (
            maximum_execution_duration_ms BETWEEN 60000 AND 86400000
        ),
    ADD CONSTRAINT schedule_occurrence_coalesce_consistency
        CHECK (
            (overlap_policy = 'QUEUE' AND coalesce = false)
            OR (overlap_policy IN ('FORBID', 'SKIP') AND coalesce = true)
        );

CREATE INDEX schedule_occurrences_open_idx
    ON control_plane.schedule_occurrences (
        organization_id,
        project_id,
        schedule_id,
        state
    )
    WHERE state IN ('QUEUED', 'CLAIMED');

-- pgvector-проекция перестраиваема и принимается только с точным digest,
-- версией ресурса и локальным происхождением модели.
CREATE TABLE control_plane.memory_vector_projections (
    resource_id uuid PRIMARY KEY REFERENCES control_plane.resources (id),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    model_id text NOT NULL CHECK (model_id ~ '^[a-z][a-z0-9._-]{0,95}$'),
    model_revision bigint NOT NULL CHECK (model_revision > 0),
    model_sha256 text NOT NULL CHECK (model_sha256 ~ '^[a-f0-9]{64}$'),
    embedding public.vector NOT NULL,
    projection_sha256 text NOT NULL CHECK (projection_sha256 ~ '^[a-f0-9]{64}$'),
    updated_at timestamptz NOT NULL,
    UNIQUE (
        organization_id,
        project_id,
        resource_id,
        resource_version,
        model_id,
        model_revision,
        model_sha256
    )
);
CREATE INDEX memory_vector_projections_scope_idx
    ON control_plane.memory_vector_projections (
        organization_id,
        project_id,
        model_id,
        model_revision,
        resource_id
    );
ALTER TABLE control_plane.memory_vector_projections ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.memory_vector_projections FORCE ROW LEVEL SECURITY;
CREATE POLICY memory_vector_projections_runtime_scope
    ON control_plane.memory_vector_projections
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS memory
             WHERE memory.id = memory_vector_projections.resource_id
               AND memory.kind = 'MEMORY_RECORD'
               AND memory.organization_id =
                   memory_vector_projections.organization_id
               AND memory.project_id = memory_vector_projections.project_id
               AND memory.version = memory_vector_projections.resource_version
               AND memory.spec ->> 'contentSHA256' =
                   memory_vector_projections.content_sha256
        )
    );
GRANT SELECT, INSERT, UPDATE ON control_plane.memory_vector_projections
    TO control_plane_runtime;

-- Верхняя граница и digest намерения переживают удаление pod и запрещают откат
-- или повторное появление уже выведенного поколения.
CREATE TABLE control_plane.runtime_principal_lifecycle (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    generation_high_watermark bigint NOT NULL CHECK (
        generation_high_watermark BETWEEN 0 AND 9007199254740991
    ),
    intent_revision bigint NOT NULL CHECK (
        intent_revision BETWEEN 1 AND 9007199254740991
    ),
    intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[a-f0-9]{64}$'),
    updated_at timestamptz NOT NULL
);
INSERT INTO control_plane.runtime_principal_lifecycle (
    singleton,
    generation_high_watermark,
    intent_revision,
    intent_sha256,
    updated_at
)
SELECT
    true,
    coalesce(max(generation), 0),
    1,
    encode(control_plane_extensions.digest(coalesce(string_agg(
        principal_name::text || ':' || generation::text || ':' || status,
        ',' ORDER BY generation
    ), ''), 'sha256'), 'hex'),
    clock_timestamp()
FROM control_plane.runtime_principals;

-- NEXT может стать CURRENT только после фактического входа и устойчивого readback
-- именно этим LOGIN.
CREATE TABLE control_plane.runtime_principal_readbacks (
    principal_name name NOT NULL,
    generation bigint NOT NULL,
    backend_pid integer NOT NULL,
    connection_started_at timestamptz NOT NULL,
    readback_at timestamptz NOT NULL,
    PRIMARY KEY (principal_name, generation),
    FOREIGN KEY (principal_name)
        REFERENCES control_plane.runtime_principals (principal_name)
);

CREATE OR REPLACE FUNCTION control_plane.register_runtime_principal_readback()
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    active_generation bigint;
BEGIN
    SELECT principal.generation
      INTO active_generation
      FROM control_plane.runtime_principals AS principal
      JOIN pg_catalog.pg_roles AS role
        ON role.rolname = principal.principal_name
     WHERE principal.principal_name::text = session_user
       AND principal.status = 'NEXT'
       AND clock_timestamp() >= principal.not_before
       AND clock_timestamp() < principal.not_after
       AND role.rolcanlogin
       AND NOT role.rolsuper
       AND NOT role.rolbypassrls
       AND pg_has_role(session_user, 'control_plane_runtime', 'member')
     FOR SHARE OF principal;
    IF active_generation IS NULL THEN
        RAISE EXCEPTION 'NEXT runtime principal is not active'
            USING ERRCODE = '28000';
    END IF;
    INSERT INTO control_plane.runtime_principal_readbacks (
        principal_name,
        generation,
        backend_pid,
        connection_started_at,
        readback_at
    ) VALUES (
        session_user,
        active_generation,
        pg_backend_pid(),
        (
            SELECT backend_start
              FROM pg_catalog.pg_stat_activity
             WHERE pid = pg_backend_pid()
        ),
        clock_timestamp()
    )
    ON CONFLICT (principal_name, generation) DO UPDATE
    SET
        backend_pid = excluded.backend_pid,
        connection_started_at = excluded.connection_started_at,
        readback_at = excluded.readback_at;
    RETURN active_generation;
END
$function$;

-- Усиленная сверка проверяет предшественника, верхнюю границу и readback, делает
-- роли RETIRED недоступными для входа, отзывает членство и завершает процессы.
CREATE OR REPLACE FUNCTION control_plane.reconcile_runtime_principals(
    requested_principals jsonb,
    requested_key_id text,
    requested_secret bytea
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    candidate record;
    candidate_count integer;
    current_count integer;
    persisted_current_generation bigint;
    requested_current_generation bigint;
    requested_max_generation bigint;
    lifecycle_high_watermark bigint;
    lifecycle_revision bigint;
    intent_digest text;
    retired_name name;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_owner', 'member')
       OR jsonb_typeof(requested_principals) <> 'array'
       OR jsonb_array_length(requested_principals) < 1
       OR jsonb_array_length(requested_principals) > 3
       OR requested_key_id !~ '^[a-z][a-z0-9._-]{0,95}$'
       OR octet_length(requested_secret) NOT BETWEEN 32 AND 128 THEN
        RAISE EXCEPTION 'runtime principal reconciliation input is invalid'
            USING ERRCODE = '22023';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM jsonb_array_elements(requested_principals) AS item(value)
         WHERE (SELECT array_agg(key ORDER BY key)
                  FROM jsonb_object_keys(item.value) AS key)
               <> ARRAY['generation', 'not_after', 'not_before', 'principal_name', 'status']::text[]
    ) THEN
        RAISE EXCEPTION 'runtime principal reconciliation fields are invalid'
            USING ERRCODE = '22023';
    END IF;

    SELECT
        count(*),
        count(*) FILTER (WHERE status = 'CURRENT'),
        max(generation),
        max(generation) FILTER (WHERE status = 'CURRENT')
      INTO
        candidate_count,
        current_count,
        requested_max_generation,
        requested_current_generation
      FROM jsonb_to_recordset(requested_principals)
        AS item(
            principal_name text,
            generation bigint,
            status text,
            not_before timestamptz,
            not_after timestamptz
        );
    IF candidate_count <> jsonb_array_length(requested_principals)
       OR current_count <> 1
       OR EXISTS (
            SELECT 1
              FROM jsonb_to_recordset(requested_principals)
                AS item(
                    principal_name text,
                    generation bigint,
                    status text,
                    not_before timestamptz,
                    not_after timestamptz
                )
             WHERE item.status NOT IN ('CURRENT', 'NEXT', 'PREVIOUS')
                OR item.generation < 1
                OR item.not_after <= item.not_before
       ) THEN
        RAISE EXCEPTION 'runtime principal lifecycle set is invalid'
            USING ERRCODE = '22023';
    END IF;

    LOCK TABLE control_plane.runtime_principals IN EXCLUSIVE MODE;
    LOCK TABLE control_plane.runtime_context_keys IN EXCLUSIVE MODE;
    SELECT generation_high_watermark, intent_revision
      INTO lifecycle_high_watermark, lifecycle_revision
      FROM control_plane.runtime_principal_lifecycle
     WHERE singleton = true
     FOR UPDATE;
    SELECT generation
      INTO persisted_current_generation
      FROM control_plane.runtime_principals
     WHERE status = 'CURRENT'
     FOR UPDATE;

    IF requested_max_generation < lifecycle_high_watermark
       OR requested_current_generation < persisted_current_generation
       OR EXISTS (
            SELECT 1
              FROM jsonb_to_recordset(requested_principals)
                AS item(
                    principal_name text,
                    generation bigint,
                    status text,
                    not_before timestamptz,
                    not_after timestamptz
                )
              LEFT JOIN control_plane.runtime_principals AS persisted
                ON persisted.generation = item.generation
             WHERE persisted.generation IS NULL
               AND item.generation <= lifecycle_high_watermark
       )
       OR EXISTS (
            SELECT 1
              FROM jsonb_to_recordset(requested_principals)
                AS item(
                    principal_name text,
                    generation bigint,
                    status text,
                    not_before timestamptz,
                    not_after timestamptz
                )
              JOIN control_plane.runtime_principals AS persisted
                ON persisted.generation = item.generation
             WHERE persisted.status = 'RETIRED'
       ) THEN
        RAISE EXCEPTION 'runtime principal rollback or resurrection is forbidden'
            USING ERRCODE = '55000';
    END IF;
    IF requested_current_generation > persisted_current_generation
       AND NOT EXISTS (
            SELECT 1
              FROM control_plane.runtime_principals AS persisted
              JOIN control_plane.runtime_principal_readbacks AS readback
                ON readback.principal_name = persisted.principal_name
               AND readback.generation = persisted.generation
             WHERE persisted.generation = requested_current_generation
               AND persisted.status = 'NEXT'
               AND readback.readback_at >= persisted.updated_at
       ) THEN
        RAISE EXCEPTION 'NEXT runtime principal readback is required'
            USING ERRCODE = '55000';
    END IF;

    FOR retired_name IN
        UPDATE control_plane.runtime_principals AS persisted
           SET status = 'RETIRED', updated_at = clock_timestamp()
         WHERE persisted.status <> 'RETIRED'
           AND NOT EXISTS (
                SELECT 1
                  FROM jsonb_to_recordset(requested_principals)
                    AS item(
                        principal_name text,
                        generation bigint,
                        status text,
                        not_before timestamptz,
                        not_after timestamptz
                    )
                 WHERE item.principal_name = persisted.principal_name::text
                   AND item.generation = persisted.generation
           )
        RETURNING principal_name
    LOOP
        EXECUTE format('ALTER ROLE %I NOLOGIN', retired_name);
        EXECUTE format('REVOKE control_plane_runtime FROM %I', retired_name);
    END LOOP;

    FOR candidate IN
        SELECT *
          FROM jsonb_to_recordset(requested_principals)
            AS item(
                principal_name text,
                generation bigint,
                status text,
                not_before timestamptz,
                not_after timestamptz
            )
         ORDER BY generation
    LOOP
        IF NOT EXISTS (
            SELECT 1
              FROM pg_catalog.pg_roles AS role
             WHERE role.rolname = candidate.principal_name
               AND role.rolcanlogin
               AND NOT role.rolsuper
               AND NOT role.rolbypassrls
               AND pg_has_role(role.rolname, 'control_plane_runtime', 'member')
        ) THEN
            RAISE EXCEPTION 'runtime principal role is invalid'
                USING ERRCODE = '22023';
        END IF;
        INSERT INTO control_plane.runtime_principals (
            principal_name,
            generation,
            status,
            not_before,
            not_after,
            updated_at
        ) VALUES (
            candidate.principal_name,
            candidate.generation,
            candidate.status,
            candidate.not_before,
            candidate.not_after,
            clock_timestamp()
        )
        ON CONFLICT (principal_name) DO UPDATE
        SET
            status = excluded.status,
            not_before = excluded.not_before,
            not_after = excluded.not_after,
            updated_at = excluded.updated_at
        WHERE control_plane.runtime_principals.generation = excluded.generation
          AND control_plane.runtime_principals.status <> 'RETIRED';
        IF NOT FOUND THEN
            RAISE EXCEPTION 'runtime principal generation mutation is forbidden'
                USING ERRCODE = '55000';
        END IF;
    END LOOP;

    UPDATE control_plane.runtime_context_keys
       SET status = 'RETIRED', updated_at = clock_timestamp()
     WHERE status = 'ACTIVE' AND key_id <> requested_key_id;
    INSERT INTO control_plane.runtime_context_keys (key_id, secret, status, updated_at)
    VALUES (requested_key_id, requested_secret, 'ACTIVE', clock_timestamp())
    ON CONFLICT (key_id) DO UPDATE
    SET secret = excluded.secret, status = 'ACTIVE', updated_at = excluded.updated_at;

    intent_digest := encode(
        control_plane_extensions.digest(
            convert_to(requested_principals::text, 'UTF8'), 'sha256'
        ),
        'hex'
    );
    UPDATE control_plane.runtime_principal_lifecycle
       SET
           generation_high_watermark =
               greatest(generation_high_watermark, requested_max_generation),
           intent_revision = lifecycle_revision + 1,
           intent_sha256 = intent_digest,
           updated_at = clock_timestamp()
     WHERE singleton = true;

    PERFORM pg_terminate_backend(activity.pid)
      FROM pg_catalog.pg_stat_activity AS activity
      JOIN control_plane.runtime_principals AS principal
        ON principal.principal_name::text = activity.usename
     WHERE principal.status = 'RETIRED'
       AND activity.pid <> pg_backend_pid();
END
$function$;

REVOKE ALL ON FUNCTION control_plane.register_runtime_principal_readback()
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.register_runtime_principal_readback()
    TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    TO control_plane_owner;

UPDATE control_plane.schema_state
   SET version = 20260731000300,
       migrated_at = clock_timestamp()
 WHERE singleton = true;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260731000300 is forward-only: downgrade would discard principal high-watermark/readback and memory projection provenance'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
