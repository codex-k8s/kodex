-- +goose Up
RESET ROLE;
GRANT pg_signal_backend TO control_plane_owner;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.runtime_principals (
    principal_name name PRIMARY KEY,
    generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
    status text NOT NULL CHECK (status IN ('CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED')),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL CHECK (not_after > not_before),
    updated_at timestamptz NOT NULL,
    UNIQUE (generation)
);
CREATE UNIQUE INDEX runtime_principals_current_uidx
    ON control_plane.runtime_principals (status)
    WHERE status = 'CURRENT';
CREATE UNIQUE INDEX runtime_principals_next_uidx
    ON control_plane.runtime_principals (status)
    WHERE status = 'NEXT';
CREATE UNIQUE INDEX runtime_principals_previous_uidx
    ON control_plane.runtime_principals (status)
    WHERE status = 'PREVIOUS';

CREATE TABLE control_plane.runtime_context_keys (
    key_id text PRIMARY KEY CHECK (key_id ~ '^[a-z][a-z0-9._-]{0,95}$'),
    secret bytea NOT NULL CHECK (octet_length(secret) BETWEEN 32 AND 128),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'RETIRED')),
    updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX runtime_context_keys_active_uidx
    ON control_plane.runtime_context_keys (status)
    WHERE status = 'ACTIVE';

CREATE TABLE control_plane.runtime_transaction_contexts (
    backend_pid integer NOT NULL,
    transaction_id bigint NOT NULL,
    principal_name name NOT NULL,
    principal_generation bigint NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    actor_id uuid NOT NULL,
    nonce uuid NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (backend_pid, transaction_id),
    FOREIGN KEY (principal_name)
        REFERENCES control_plane.runtime_principals (principal_name)
);
CREATE INDEX runtime_transaction_contexts_expiry_idx
    ON control_plane.runtime_transaction_contexts (expires_at);

CREATE OR REPLACE FUNCTION control_plane.activate_runtime_context(
    requested_organization_id uuid,
    requested_project_id uuid,
    requested_actor_id uuid,
    requested_principal_name name,
    requested_principal_generation bigint,
    requested_key_id text,
    requested_nonce uuid,
    requested_expires_unix_micro bigint,
    requested_signature bytea
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    active_secret bytea;
    canonical text;
    expires_at timestamptz;
BEGIN
    expires_at := to_timestamp(requested_expires_unix_micro::numeric / 1000000);
    IF requested_principal_name::text <> session_user
       OR requested_expires_unix_micro <= floor(extract(epoch FROM clock_timestamp()) * 1000000)
       OR requested_expires_unix_micro > floor(extract(epoch FROM clock_timestamp() + interval '10 seconds') * 1000000)
       OR NOT pg_has_role(session_user, 'control_plane_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime context identity is invalid' USING ERRCODE = '28000';
    END IF;

    SELECT context_key.secret
      INTO active_secret
      FROM control_plane.runtime_context_keys AS context_key
     WHERE context_key.key_id = requested_key_id
       AND context_key.status = 'ACTIVE'
     FOR SHARE;
    IF active_secret IS NULL THEN
        RAISE EXCEPTION 'runtime context key is unavailable' USING ERRCODE = '28000';
    END IF;

    PERFORM 1
      FROM control_plane.runtime_principals AS principal
      JOIN pg_catalog.pg_roles AS role
        ON role.rolname = principal.principal_name
     WHERE principal.principal_name = requested_principal_name
       AND principal.generation = requested_principal_generation
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before
       AND clock_timestamp() < principal.not_after
       AND role.rolcanlogin
       AND NOT role.rolsuper
       AND NOT role.rolbypassrls
       AND pg_has_role(role.rolname, 'control_plane_runtime', 'member')
     FOR SHARE OF principal;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;

    canonical := 'v1' || chr(10)
        || requested_principal_name::text || chr(10)
        || requested_principal_generation::text || chr(10)
        || requested_organization_id::text || chr(10)
        || coalesce(requested_project_id::text, '') || chr(10)
        || requested_actor_id::text || chr(10)
        || requested_nonce::text || chr(10)
        || requested_expires_unix_micro::text;
    IF hmac(convert_to(canonical, 'UTF8'), active_secret, 'sha256')
       <> requested_signature THEN
        RAISE EXCEPTION 'runtime context signature is invalid' USING ERRCODE = '28000';
    END IF;

    DELETE FROM control_plane.runtime_transaction_contexts
     WHERE ctid IN (
        SELECT expired.ctid
          FROM control_plane.runtime_transaction_contexts AS expired
         WHERE expired.expires_at < clock_timestamp() - interval '1 minute'
         ORDER BY expired.expires_at
         LIMIT 1000
     );

    INSERT INTO control_plane.runtime_transaction_contexts (
        backend_pid,
        transaction_id,
        principal_name,
        principal_generation,
        organization_id,
        project_id,
        actor_id,
        nonce,
        expires_at,
        created_at
    ) VALUES (
        pg_backend_pid(),
        txid_current(),
        requested_principal_name,
        requested_principal_generation,
        requested_organization_id,
        requested_project_id,
        requested_actor_id,
        requested_nonce,
        expires_at,
        clock_timestamp()
    );
END
$function$;

CREATE OR REPLACE FUNCTION control_plane.runtime_scope(
    OUT organization_id uuid,
    OUT project_id uuid,
    OUT actor_id uuid
) RETURNS record
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    SELECT context.organization_id, context.project_id, context.actor_id
      INTO organization_id, project_id, actor_id
      FROM control_plane.runtime_transaction_contexts AS context
      JOIN control_plane.runtime_principals AS principal
        ON principal.principal_name = context.principal_name
       AND principal.generation = context.principal_generation
     WHERE context.backend_pid = pg_backend_pid()
       AND context.transaction_id = txid_current()
       AND context.principal_name::text = session_user
       AND context.expires_at > clock_timestamp()
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before
       AND clock_timestamp() < principal.not_after
       AND pg_has_role(session_user, 'control_plane_runtime', 'member');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime context is not active' USING ERRCODE = '28000';
    END IF;
END
$function$;

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

    SELECT count(*), count(*) FILTER (WHERE status = 'CURRENT')
      INTO candidate_count, current_count
      FROM jsonb_to_recordset(requested_principals)
        AS item(
            principal_name text,
            generation bigint,
            status text,
            not_before timestamptz,
            not_after timestamptz
        );
    IF candidate_count <> jsonb_array_length(requested_principals)
       OR current_count <> 1 THEN
        RAISE EXCEPTION 'runtime principal lifecycle set is invalid'
            USING ERRCODE = '22023';
    END IF;

    LOCK TABLE control_plane.runtime_principals IN EXCLUSIVE MODE;
    LOCK TABLE control_plane.runtime_context_keys IN EXCLUSIVE MODE;
    UPDATE control_plane.runtime_principals
       SET status = 'RETIRED', updated_at = clock_timestamp()
     WHERE status <> 'RETIRED';

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
    LOOP
        IF candidate.status NOT IN ('CURRENT', 'NEXT', 'PREVIOUS')
           OR candidate.generation < 1
           OR candidate.not_after <= candidate.not_before
           OR NOT EXISTS (
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
            generation = excluded.generation,
            status = excluded.status,
            not_before = excluded.not_before,
            not_after = excluded.not_after,
            updated_at = excluded.updated_at;
    END LOOP;

    UPDATE control_plane.runtime_context_keys
       SET status = 'RETIRED', updated_at = clock_timestamp()
     WHERE status = 'ACTIVE' AND key_id <> requested_key_id;
    INSERT INTO control_plane.runtime_context_keys (key_id, secret, status, updated_at)
    VALUES (requested_key_id, requested_secret, 'ACTIVE', clock_timestamp())
    ON CONFLICT (key_id) DO UPDATE
    SET secret = excluded.secret, status = 'ACTIVE', updated_at = excluded.updated_at;

    PERFORM pg_terminate_backend(activity.pid)
      FROM pg_catalog.pg_stat_activity AS activity
      JOIN control_plane.runtime_principals AS principal
        ON principal.principal_name::text = activity.usename
     WHERE principal.status = 'RETIRED'
       AND activity.pid <> pg_backend_pid();
END
$function$;

CREATE OR REPLACE FUNCTION control_plane.runtime_identity()
RETURNS TABLE (
    schema_version bigint,
    principal_status text,
    principal_generation bigint,
    login_enabled boolean,
    non_superuser boolean,
    no_bypass_rls boolean
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
    SELECT
        state.version,
        principal.status,
        principal.generation,
        role.rolcanlogin,
        NOT role.rolsuper,
        NOT role.rolbypassrls
    FROM control_plane.schema_state AS state
    JOIN control_plane.runtime_principals AS principal
      ON principal.principal_name::text = session_user
    JOIN pg_catalog.pg_roles AS role
      ON role.rolname = principal.principal_name
    WHERE state.singleton = true
      AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
      AND clock_timestamp() >= principal.not_before
      AND clock_timestamp() < principal.not_after
      AND pg_has_role(session_user, 'control_plane_runtime', 'member')
$function$;

CREATE OR REPLACE FUNCTION control_plane.safe_diagnostics()
RETURNS TABLE (
    schema_version bigint,
    pending_outbox_events bigint,
    terminal_outbox_events bigint,
    oldest_pending_seconds double precision,
    active_turn_leases bigint,
    queued_schedule_occurrences bigint,
    runtime_principal_status text,
    runtime_principal_generation bigint
)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    verified_scope record;
BEGIN
    SELECT * INTO verified_scope FROM control_plane.runtime_scope();
    RETURN QUERY
    SELECT
        state.version,
        count(*) FILTER (
            WHERE outbox.published_at IS NULL AND NOT outbox.terminal
        ),
        count(*) FILTER (
            WHERE outbox.published_at IS NULL AND outbox.terminal
        ),
        coalesce(max(
            extract(epoch FROM clock_timestamp() - outbox.occurred_at)
        ) FILTER (
            WHERE outbox.published_at IS NULL AND NOT outbox.terminal
        ), 0),
        (
            SELECT count(*)
              FROM control_plane.turn_leases AS lease
             WHERE lease.expires_at > clock_timestamp()
        ),
        (
            SELECT count(*)
              FROM control_plane.schedule_occurrences AS occurrence
             WHERE occurrence.organization_id = verified_scope.organization_id
               AND occurrence.project_id = verified_scope.project_id
               AND occurrence.state = 'QUEUED'
        ),
        principal.status,
        principal.generation
    FROM control_plane.schema_state AS state
    JOIN control_plane.runtime_principals AS principal
      ON principal.principal_name::text = session_user
    LEFT JOIN control_plane.outbox_events AS outbox
      ON outbox.organization_id = verified_scope.organization_id
     AND outbox.project_id = verified_scope.project_id
    WHERE state.singleton = true
    GROUP BY state.version, principal.status, principal.generation;
END
$function$;

ALTER TABLE control_plane.turn_leases
    ADD COLUMN authority_generation bigint NOT NULL DEFAULT 1
        CHECK (authority_generation BETWEEN 1 AND 9007199254740991),
    ADD COLUMN attempt integer NOT NULL DEFAULT 1
        CHECK (attempt BETWEEN 1 AND 100);

CREATE TABLE control_plane.turn_attempts (
    turn_id uuid NOT NULL,
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    workload_id text NOT NULL CHECK (workload_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    authority_generation bigint NOT NULL CHECK (
        authority_generation BETWEEN 1 AND 9007199254740991
    ),
    state text NOT NULL CHECK (state IN (
        'QUEUED', 'CLAIMED', 'EXPIRED', 'SUCCEEDED', 'FAILED', 'CANCELLED'
    )),
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[a-f0-9]{64}$'),
    lease_fence bigint NOT NULL CHECK (lease_fence BETWEEN 1 AND 9007199254740991),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    outcome text,
    PRIMARY KEY (turn_id, attempt),
    CHECK ((finished_at IS NULL) = (state IN ('QUEUED', 'CLAIMED')))
);
CREATE INDEX turn_attempts_state_idx
    ON control_plane.turn_attempts (state, started_at, turn_id);

CREATE UNIQUE INDEX resources_one_active_turn_per_session_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        ((spec ->> 'sessionId')::uuid)
    )
    WHERE kind = 'TURN'
      AND state IN (
        'CLAIMED', 'RUNNING', 'WAITING_OWNER', 'WAITING_EXTERNAL', 'BLOCKED'
      );

ALTER TABLE control_plane.schedule_occurrences
    ADD COLUMN target_kind text NOT NULL DEFAULT 'PROCESS_RUN',
    ADD COLUMN target_version bigint NOT NULL DEFAULT 1 CHECK (target_version > 0),
    ADD COLUMN effective_input_sha256 text NOT NULL
        DEFAULT repeat('0', 64) CHECK (effective_input_sha256 ~ '^[a-f0-9]{64}$'),
    ADD COLUMN overlap_policy text NOT NULL DEFAULT 'FORBID' CHECK (
        overlap_policy IN ('FORBID', 'SKIP', 'QUEUE')
    ),
    ADD COLUMN maximum_attempts integer NOT NULL DEFAULT 1 CHECK (
        maximum_attempts BETWEEN 1 AND 100
    ),
    ADD COLUMN initial_backoff_ms bigint NOT NULL DEFAULT 1000 CHECK (
        initial_backoff_ms BETWEEN 1000 AND 86400000
    ),
    ADD COLUMN maximum_backoff_ms bigint NOT NULL DEFAULT 1000 CHECK (
        maximum_backoff_ms BETWEEN initial_backoff_ms AND 86400000
    ),
    ADD COLUMN dead_letter_at timestamptz NOT NULL DEFAULT 'infinity',
    ADD COLUMN state text NOT NULL DEFAULT 'QUEUED' CHECK (state IN (
        'QUEUED', 'CLAIMED', 'SUCCEEDED', 'FAILED',
        'CANCELLED', 'SKIPPED', 'DEAD_LETTER'
    )),
    ADD COLUMN attempt integer NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 100),
    ADD COLUMN claimant_workload_id text,
    ADD COLUMN authority_generation bigint,
    ADD COLUMN token_hash text,
    ADD COLUMN lease_expires_at timestamptz,
    ADD COLUMN available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ADD COLUMN outcome text,
    ADD COLUMN result_artifact_id uuid,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ADD CONSTRAINT schedule_occurrence_lease_consistency CHECK (
        (
            state = 'CLAIMED'
            AND claimant_workload_id IS NOT NULL
            AND authority_generation IS NOT NULL
            AND token_hash ~ '^[a-f0-9]{64}$'
            AND lease_expires_at IS NOT NULL
        )
        OR (
            state <> 'CLAIMED'
            AND claimant_workload_id IS NULL
            AND authority_generation IS NULL
            AND token_hash IS NULL
            AND lease_expires_at IS NULL
        )
    );
CREATE INDEX schedule_occurrences_claim_idx
    ON control_plane.schedule_occurrences (
        organization_id,
        project_id,
        available_at,
        scheduled_for,
        id
    )
    WHERE state = 'QUEUED';

ALTER TABLE control_plane.outbox_events
    ADD COLUMN ordering_key text GENERATED ALWAYS AS (
        organization_id::text || ':' || event_name || ':' ||
        aggregate_type || ':' || aggregate_id::text
    ) STORED,
    ADD COLUMN broker_stream text,
    ADD COLUMN broker_sequence bigint,
    ADD COLUMN broker_duplicate boolean,
    ADD COLUMN delivery_receipt_at timestamptz,
    ADD COLUMN cleanup_after timestamptz,
    ADD CONSTRAINT outbox_delivery_receipt_consistency CHECK (
        (
            published_at IS NULL
            AND broker_stream IS NULL
            AND broker_sequence IS NULL
            AND broker_duplicate IS NULL
            AND delivery_receipt_at IS NULL
            AND cleanup_after IS NULL
        )
        OR (
            published_at IS NOT NULL
            AND broker_stream IS NOT NULL
            AND broker_sequence > 0
            AND broker_duplicate IS NOT NULL
            AND delivery_receipt_at IS NOT NULL
            AND cleanup_after > delivery_receipt_at
        )
    );
CREATE INDEX outbox_events_ordering_idx
    ON control_plane.outbox_events (ordering_key, event_sequence);
CREATE INDEX outbox_events_cleanup_idx
    ON control_plane.outbox_events (cleanup_after, event_id)
    WHERE published_at IS NOT NULL;

DROP POLICY resources_runtime_scope ON control_plane.resources;
DROP POLICY receipts_runtime_scope ON control_plane.command_receipts;
DROP POLICY audit_runtime_scope ON control_plane.audit_events;
DROP POLICY outbox_runtime_insert ON control_plane.outbox_events;
DROP POLICY turn_leases_runtime_scope ON control_plane.turn_leases;
DROP POLICY schedule_occurrences_runtime_scope ON control_plane.schedule_occurrences;
DROP POLICY cache_epochs_runtime_scope ON control_plane.cache_epochs;
DROP POLICY project_actor_permissions_runtime_read ON control_plane.project_actor_permissions;
DROP POLICY project_actor_permissions_runtime_write ON control_plane.project_actor_permissions;

CREATE POLICY resources_runtime_scope ON control_plane.resources
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (
            project_id = (control_plane.runtime_scope()).project_id
            OR (
                (control_plane.runtime_scope()).project_id IS NULL
                AND kind = 'PROJECT'
            )
        )
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (
            project_id = (control_plane.runtime_scope()).project_id
            OR (
                (control_plane.runtime_scope()).project_id IS NULL
                AND kind = 'PROJECT'
                AND project_id = id
            )
        )
    );

CREATE POLICY receipts_runtime_scope ON control_plane.command_receipts
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id IS NOT DISTINCT FROM (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id IS NOT DISTINCT FROM (control_plane.runtime_scope()).project_id
    );

CREATE POLICY audit_runtime_scope ON control_plane.audit_events
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND actor_id = (control_plane.runtime_scope()).actor_id
    );

CREATE POLICY outbox_runtime_insert ON control_plane.outbox_events
    FOR INSERT TO control_plane_runtime
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );

CREATE POLICY turn_leases_runtime_scope ON control_plane.turn_leases
    FOR ALL TO control_plane_runtime
    USING (
        EXISTS (
            SELECT 1
              FROM control_plane.resources AS resource
             WHERE resource.id = turn_leases.turn_id
        )
    )
    WITH CHECK (
        EXISTS (
            SELECT 1
              FROM control_plane.resources AS resource
             WHERE resource.id = turn_leases.turn_id
        )
    );

ALTER TABLE control_plane.turn_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.turn_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY turn_attempts_runtime_scope ON control_plane.turn_attempts
    FOR ALL TO control_plane_runtime
    USING (
        EXISTS (
            SELECT 1
              FROM control_plane.resources AS resource
             WHERE resource.id = turn_attempts.turn_id
        )
    )
    WITH CHECK (
        EXISTS (
            SELECT 1
              FROM control_plane.resources AS resource
             WHERE resource.id = turn_attempts.turn_id
        )
    );

CREATE POLICY schedule_occurrences_runtime_scope
    ON control_plane.schedule_occurrences
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );

CREATE POLICY cache_epochs_runtime_scope ON control_plane.cache_epochs
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (
            project_id IS NULL
            OR project_id = (control_plane.runtime_scope()).project_id
        )
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (
            project_id IS NULL
            OR project_id = (control_plane.runtime_scope()).project_id
        )
    );

CREATE POLICY project_actor_permissions_runtime_read
    ON control_plane.project_actor_permissions
    FOR SELECT TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND actor_id = (control_plane.runtime_scope()).actor_id
    );
CREATE POLICY project_actor_permissions_runtime_write
    ON control_plane.project_actor_permissions
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (
            project_id = (control_plane.runtime_scope()).project_id
            OR (
                (control_plane.runtime_scope()).project_id IS NULL
                AND EXISTS (
                    SELECT 1
                      FROM control_plane.resources AS project
                     WHERE project.id = project_actor_permissions.project_id
                       AND project.organization_id =
                           project_actor_permissions.organization_id
                       AND project.kind = 'PROJECT'
                )
            )
        )
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (
            project_id = (control_plane.runtime_scope()).project_id
            OR (
                (control_plane.runtime_scope()).project_id IS NULL
                AND EXISTS (
                    SELECT 1
                      FROM control_plane.resources AS project
                     WHERE project.id = project_actor_permissions.project_id
                       AND project.organization_id =
                           project_actor_permissions.organization_id
                       AND project.kind = 'PROJECT'
                )
            )
        )
    );

GRANT EXECUTE ON FUNCTION control_plane.activate_runtime_context(
    uuid, uuid, uuid, name, bigint, text, uuid, bigint, bytea
) TO control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.runtime_identity() TO control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.safe_diagnostics() TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.runtime_scope() FROM PUBLIC;
REVOKE ALL ON FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    TO control_plane_owner;
GRANT SELECT, INSERT, UPDATE ON control_plane.turn_attempts TO control_plane_runtime;
GRANT SELECT ON control_plane.audit_events TO control_plane_runtime;

UPDATE control_plane.schema_state
   SET version = 20260731000200,
       migrated_at = clock_timestamp()
 WHERE singleton = true;

RESET ROLE;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260731000200 is forward-only: downgrade would restore caller-controlled RLS context and discard durable delivery/attempt evidence'
        USING ERRCODE = '0A000';
END
$forward_only$;
