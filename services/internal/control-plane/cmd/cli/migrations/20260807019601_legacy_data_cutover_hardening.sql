-- +goose Up
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

-- Receipt tuple становится неизменяемым сразу после PREPARED. Migration
-- identity вызывает только закрытые owner capabilities и не получает DML.
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON control_plane.legacy_data_cutovers
    FROM control_plane_migration;
REVOKE UPDATE ON control_plane.legacy_data_cutovers
    FROM control_plane_legacy_materializer;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_legacy_data_cutover_transition()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF ROW(OLD.plan_id, OLD.plan_sha256, OLD.source_sha256, OLD.target_sha256,
           OLD.backup_sha256, OLD.manifest_sha256, OLD.materialization_sha256,
           OLD.materialization_count, OLD.materialization_plan, OLD.mapping_counts,
           OLD.prepared_at)
       IS DISTINCT FROM
       ROW(NEW.plan_id, NEW.plan_sha256, NEW.source_sha256, NEW.target_sha256,
           NEW.backup_sha256, NEW.manifest_sha256, NEW.materialization_sha256,
           NEW.materialization_count, NEW.materialization_plan, NEW.mapping_counts,
           NEW.prepared_at) THEN
        RAISE EXCEPTION 'legacy cutover immutable receipt mutation is forbidden'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.state IN ('COMMITTED', 'ABORTED') THEN
        RAISE EXCEPTION 'legacy cutover terminal receipt is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.state <> 'PREPARED' OR NEW.state NOT IN ('PREPARED', 'COMMITTED', 'ABORTED') THEN
        RAISE EXCEPTION 'legacy cutover transition is invalid' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.guard_legacy_data_cutover_transition() OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.guard_legacy_data_cutover_transition() FROM PUBLIC;
CREATE TRIGGER legacy_data_cutovers_immutable_transition
BEFORE UPDATE ON control_plane.legacy_data_cutovers
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_legacy_data_cutover_transition();

-- +goose StatementBegin
CREATE FUNCTION control_plane.prepare_legacy_data_cutover(
    requested_plan_id text, requested_plan_sha256 text, requested_source_sha256 text,
    requested_target_sha256 text, requested_backup_sha256 text, requested_manifest_sha256 text,
    requested_materialization_sha256 text, requested_materialization_count bigint,
    requested_materialization_plan text, requested_mapping_counts jsonb
) RETURNS TABLE (
    plan_id text, plan_sha256 text, source_sha256 text, target_sha256 text,
    backup_sha256 text, manifest_sha256 text, materialization_sha256 text,
    materialization_count bigint, state text, restore_verified boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE stored control_plane.legacy_data_cutovers%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy cutover caller is invalid' USING ERRCODE = '28000';
    END IF;
    INSERT INTO control_plane.legacy_data_cutovers (
        plan_id, plan_sha256, source_sha256, target_sha256, backup_sha256,
        manifest_sha256, materialization_sha256, materialization_count,
        materialization_plan, mapping_counts, state, prepared_at
    ) VALUES (
        requested_plan_id, requested_plan_sha256, requested_source_sha256,
        requested_target_sha256, requested_backup_sha256, requested_manifest_sha256,
        requested_materialization_sha256, requested_materialization_count,
        requested_materialization_plan, requested_mapping_counts, 'PREPARED', transaction_timestamp()
    ) ON CONFLICT (plan_id) DO NOTHING;
    SELECT * INTO STRICT stored FROM control_plane.legacy_data_cutovers AS receipt
    WHERE receipt.plan_id = requested_plan_id;
    IF stored.plan_sha256 <> requested_plan_sha256
       OR stored.source_sha256 <> requested_source_sha256
       OR stored.target_sha256 <> requested_target_sha256
       OR stored.backup_sha256 <> requested_backup_sha256
       OR stored.manifest_sha256 <> requested_manifest_sha256
       OR stored.materialization_sha256 <> requested_materialization_sha256
       OR stored.materialization_count <> requested_materialization_count
       OR stored.materialization_plan <> requested_materialization_plan
       OR stored.mapping_counts <> requested_mapping_counts
       OR stored.state NOT IN ('PREPARED', 'COMMITTED') THEN
        RAISE EXCEPTION 'legacy cutover immutable receipt mismatch' USING ERRCODE = '55000';
    END IF;
    RETURN QUERY SELECT stored.plan_id, stored.plan_sha256, stored.source_sha256,
        stored.target_sha256, stored.backup_sha256, stored.manifest_sha256,
        stored.materialization_sha256, stored.materialization_count, stored.state,
        stored.restore_verified_at IS NOT NULL;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.prepare_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint, text, jsonb
) OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.prepare_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint, text, jsonb
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.prepare_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint, text, jsonb
) TO control_plane_migration;

-- +goose StatementBegin
CREATE FUNCTION control_plane.verify_legacy_data_cutover_restore(
    requested_plan_id text, requested_plan_sha256 text, requested_source_sha256 text,
    requested_target_sha256 text, requested_backup_sha256 text, requested_manifest_sha256 text,
    requested_materialization_sha256 text, requested_materialization_count bigint
) RETURNS TABLE (
    plan_id text, plan_sha256 text, source_sha256 text, target_sha256 text,
    backup_sha256 text, manifest_sha256 text, materialization_sha256 text,
    materialization_count bigint, state text, restore_verified boolean
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE stored control_plane.legacy_data_cutovers%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy cutover caller is invalid' USING ERRCODE = '28000';
    END IF;
    UPDATE control_plane.legacy_data_cutovers AS receipt
    SET restore_verified_at = coalesce(receipt.restore_verified_at, transaction_timestamp())
    WHERE receipt.plan_id = requested_plan_id AND receipt.plan_sha256 = requested_plan_sha256
      AND receipt.source_sha256 = requested_source_sha256 AND receipt.target_sha256 = requested_target_sha256
      AND receipt.backup_sha256 = requested_backup_sha256 AND receipt.manifest_sha256 = requested_manifest_sha256
      AND receipt.materialization_sha256 = requested_materialization_sha256
      AND receipt.materialization_count = requested_materialization_count AND receipt.state = 'PREPARED';
    IF NOT FOUND THEN RAISE EXCEPTION 'legacy cutover restore receipt mismatch' USING ERRCODE = '55000'; END IF;
    SELECT * INTO STRICT stored FROM control_plane.legacy_data_cutovers AS receipt
    WHERE receipt.plan_id = requested_plan_id;
    RETURN QUERY SELECT stored.plan_id, stored.plan_sha256, stored.source_sha256,
        stored.target_sha256, stored.backup_sha256, stored.manifest_sha256,
        stored.materialization_sha256, stored.materialization_count, stored.state, true;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.verify_legacy_data_cutover_restore(
    text, text, text, text, text, text, text, bigint
) OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.verify_legacy_data_cutover_restore(
    text, text, text, text, text, text, text, bigint
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.verify_legacy_data_cutover_restore(
    text, text, text, text, text, text, text, bigint
) TO control_plane_migration;

-- +goose StatementBegin
CREATE FUNCTION control_plane.abort_legacy_data_cutover(
    requested_plan_id text, requested_plan_sha256 text, requested_source_sha256 text,
    requested_target_sha256 text, requested_backup_sha256 text, requested_manifest_sha256 text,
    requested_materialization_sha256 text, requested_materialization_count bigint
) RETURNS TABLE (
    plan_id text, plan_sha256 text, source_sha256 text, target_sha256 text,
    backup_sha256 text, manifest_sha256 text, materialization_sha256 text,
    materialization_count bigint, state text, restore_verified boolean
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE stored control_plane.legacy_data_cutovers%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy cutover caller is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT * INTO STRICT stored FROM control_plane.legacy_data_cutovers AS receipt
    WHERE receipt.plan_id = requested_plan_id FOR UPDATE;
    IF stored.state = 'ABORTED'
       AND stored.plan_sha256 = requested_plan_sha256 AND stored.source_sha256 = requested_source_sha256
       AND stored.target_sha256 = requested_target_sha256 AND stored.backup_sha256 = requested_backup_sha256
       AND stored.manifest_sha256 = requested_manifest_sha256
       AND stored.materialization_sha256 = requested_materialization_sha256
       AND stored.materialization_count = requested_materialization_count THEN
        RETURN QUERY SELECT stored.plan_id, stored.plan_sha256, stored.source_sha256,
            stored.target_sha256, stored.backup_sha256, stored.manifest_sha256,
            stored.materialization_sha256, stored.materialization_count, stored.state,
            stored.restore_verified_at IS NOT NULL;
        RETURN;
    END IF;
    IF stored.plan_sha256 <> requested_plan_sha256 OR stored.source_sha256 <> requested_source_sha256
       OR stored.target_sha256 <> requested_target_sha256 OR stored.backup_sha256 <> requested_backup_sha256
       OR stored.manifest_sha256 <> requested_manifest_sha256
       OR stored.materialization_sha256 <> requested_materialization_sha256
       OR stored.materialization_count <> requested_materialization_count
       OR stored.state <> 'PREPARED' OR stored.materialization_running THEN
        RAISE EXCEPTION 'legacy cutover abort receipt mismatch' USING ERRCODE = '55000';
    END IF;
    UPDATE control_plane.legacy_data_cutovers AS receipt
    SET state = 'ABORTED', aborted_at = transaction_timestamp()
    WHERE receipt.plan_id = requested_plan_id AND receipt.state = 'PREPARED';
    SELECT * INTO STRICT stored FROM control_plane.legacy_data_cutovers AS receipt
    WHERE receipt.plan_id = requested_plan_id;
    RETURN QUERY SELECT stored.plan_id, stored.plan_sha256, stored.source_sha256,
        stored.target_sha256, stored.backup_sha256, stored.manifest_sha256,
        stored.materialization_sha256, stored.materialization_count, stored.state,
        stored.restore_verified_at IS NOT NULL;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.abort_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.abort_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.abort_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) TO control_plane_migration;

-- +goose StatementBegin
CREATE FUNCTION control_plane.begin_legacy_data_cutover_materialization(requested_plan_id text)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy materialization caller is invalid' USING ERRCODE = '28000';
    END IF;
    UPDATE control_plane.legacy_data_cutovers AS receipt SET materialization_running = true
    WHERE receipt.plan_id = requested_plan_id AND receipt.state = 'PREPARED'
      AND receipt.restore_verified_at IS NOT NULL AND NOT receipt.materialization_running;
    IF NOT FOUND THEN RAISE EXCEPTION 'legacy materialization fence is occupied' USING ERRCODE = '40001'; END IF;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.begin_legacy_data_cutover_materialization(text) OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.begin_legacy_data_cutover_materialization(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.begin_legacy_data_cutover_materialization(text)
    TO control_plane_legacy_materializer;

-- +goose StatementBegin
CREATE FUNCTION control_plane.complete_legacy_data_cutover_materialization(requested_plan_id text)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy materialization caller is invalid' USING ERRCODE = '28000';
    END IF;
    UPDATE control_plane.legacy_data_cutovers AS receipt
    SET state = 'COMMITTED', committed_at = transaction_timestamp(), materialization_running = false
    WHERE receipt.plan_id = requested_plan_id AND receipt.state = 'PREPARED'
      AND receipt.materialization_running;
    IF NOT FOUND THEN RAISE EXCEPTION 'legacy materialization commit lost its fence' USING ERRCODE = '40001'; END IF;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.complete_legacy_data_cutover_materialization(text) OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.complete_legacy_data_cutover_materialization(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.complete_legacy_data_cutover_materialization(text)
    TO control_plane_legacy_materializer;

CREATE TABLE control_plane.legacy_data_cutover_provenance (
    plan_id text NOT NULL REFERENCES control_plane.legacy_data_cutovers(plan_id),
    process_run_id uuid NOT NULL REFERENCES control_plane.resources(id),
    source_table text NOT NULL CHECK (source_table = 'matter_codex_process_runs'),
    source_id bigint NOT NULL CHECK (source_id > 0),
    source_digest text NOT NULL CHECK (source_digest ~ '^[a-f0-9]{64}$'),
    target_actor_id uuid NOT NULL,
    provenance jsonb NOT NULL CHECK (
        jsonb_typeof(provenance) = 'object'
        AND provenance->>'rootActorSourceRef' <> ''
        AND (provenance->>'policyRevision')::bigint > 0
        AND provenance->>'policySha256' ~ '^[a-f0-9]{64}$'
        AND octet_length(provenance::text) <= 16384
    ),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (plan_id, process_run_id),
    UNIQUE (plan_id, source_table, source_id)
);
ALTER TABLE control_plane.legacy_data_cutover_provenance OWNER TO control_plane_owner;
REVOKE ALL ON control_plane.legacy_data_cutover_provenance FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.legacy_data_cutover_provenance
    TO control_plane_legacy_materializer;

GRANT INSERT ON control_plane.turn_attempts TO control_plane_legacy_materializer;
GRANT UPDATE ON control_plane.resources TO control_plane_legacy_materializer;
CREATE POLICY turn_attempts_legacy_materialization_owner
    ON control_plane.turn_attempts FOR INSERT TO control_plane_legacy_materializer
    WITH CHECK (pg_has_role(session_user, 'control_plane_migration', 'member'));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.materialize_legacy_data_cutover(
    requested_plan_id text, requested_plan_sha256 text, requested_source_sha256 text,
    requested_target_sha256 text, requested_backup_sha256 text, requested_manifest_sha256 text,
    requested_materialization_sha256 text, requested_materialization_count bigint
) RETURNS TABLE (
    plan_id text, plan_sha256 text, source_sha256 text, target_sha256 text,
    backup_sha256 text, manifest_sha256 text, materialization_sha256 text,
    materialization_count bigint, state text, restore_verified boolean
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, control_plane
SET row_security = on
AS $function$
DECLARE
    cutover control_plane.legacy_data_cutovers%ROWTYPE;
    item jsonb;
    operation text;
    expected control_plane.resources%ROWTYPE;
    stored control_plane.resources%ROWTYPE;
    project control_plane.resources%ROWTYPE;
    history control_plane.protected_resource_history%ROWTYPE;
    provenance jsonb;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy materialization caller is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT * INTO STRICT cutover FROM control_plane.legacy_data_cutovers AS receipt
    WHERE receipt.plan_id = requested_plan_id;
    IF cutover.plan_sha256 <> requested_plan_sha256 OR cutover.source_sha256 <> requested_source_sha256
       OR cutover.target_sha256 <> requested_target_sha256 OR cutover.backup_sha256 <> requested_backup_sha256
       OR cutover.manifest_sha256 <> requested_manifest_sha256
       OR cutover.materialization_sha256 <> requested_materialization_sha256
       OR cutover.materialization_count <> requested_materialization_count
       OR cutover.restore_verified_at IS NULL OR cutover.state NOT IN ('PREPARED', 'COMMITTED') THEN
        RAISE EXCEPTION 'legacy materialization receipt is invalid' USING ERRCODE = '55000';
    END IF;
    -- Replay after the terminal winner is read-only: no GUC, loop or DML.
    IF cutover.state = 'COMMITTED' THEN
        IF EXISTS (
            SELECT 1
            FROM jsonb_array_elements(cutover.materialization_plan::jsonb) AS command(value)
            WHERE command.value->>'operation' = 'UPSERT_PROCESS_RUN'
              AND NOT EXISTS (
                  SELECT 1
                  FROM control_plane.legacy_data_cutover_provenance AS receipt
                  JOIN control_plane.resources AS process ON process.id = receipt.process_run_id
                  WHERE receipt.plan_id = cutover.plan_id
                    AND receipt.process_run_id = (command.value->>'targetId')::uuid
                    AND receipt.source_table = command.value->>'sourceTable'
                    AND receipt.source_id = (command.value->>'sourceId')::bigint
                    AND receipt.source_digest = command.value->>'sourceDigest'
                    AND receipt.target_actor_id = process.owner_actor_id
                    AND receipt.provenance = command.value->'processProvenance'
              )
        ) THEN
            RAISE EXCEPTION 'legacy committed provenance readback mismatch' USING ERRCODE = '55000';
        END IF;
        RETURN QUERY SELECT cutover.plan_id, cutover.plan_sha256, cutover.source_sha256,
            cutover.target_sha256, cutover.backup_sha256, cutover.manifest_sha256,
            cutover.materialization_sha256, cutover.materialization_count, cutover.state, true;
        RETURN;
    END IF;
    PERFORM control_plane.begin_legacy_data_cutover_materialization(cutover.plan_id);
    PERFORM set_config('mattercodex.legacy_materialization_plan_id', cutover.plan_id, true);

    FOR item IN SELECT value FROM jsonb_array_elements(cutover.materialization_plan::jsonb) WITH ORDINALITY
        AS command(value, ordinality) ORDER BY ordinality
    LOOP
        operation := item->>'operation';
        IF operation NOT IN ('UPSERT_PROJECT', 'UPSERT_TEAM', 'UPSERT_CHAT',
            'UPSERT_PROTECTED_CONFIGURATION', 'UPSERT_SESSION', 'UPSERT_TURN',
            'UPSERT_TURN_ATTEMPT', 'UPSERT_PROCESS_RUN', 'UPSERT_SCHEDULE')
           OR item->>'sourceDigest' !~ '^[a-f0-9]{64}$'
           OR (item->>'sourceId')::bigint < 1 OR (item->>'sourceRevision')::bigint < 1
           OR (operation <> 'UPSERT_TURN_ATTEMPT' AND item->>'targetId' !~ '^[a-f0-9-]{36}$')
           OR (operation = 'UPSERT_TURN_ATTEMPT' AND item->>'targetId' !~ '^[a-f0-9-]{36}#([1-9][0-9]?|100)$')
           OR item->>'projectTargetId' !~ '^[a-f0-9-]{36}$'
           OR jsonb_typeof(item->'resource') <> 'object'
           OR item->>'targetKind' = '' THEN
            RAISE EXCEPTION 'legacy materialization command is invalid' USING ERRCODE = '22023';
        END IF;
        IF NOT CASE operation
            WHEN 'UPSERT_PROJECT' THEN item->>'sourceTable' = 'matter_codex_projects' AND item->>'targetKind' = 'PROJECT'
            WHEN 'UPSERT_TEAM' THEN item->>'sourceTable' = 'matter_codex_projects' AND item->>'targetKind' = 'TEAM'
            WHEN 'UPSERT_CHAT' THEN item->>'sourceTable' = 'matter_codex_chats' AND item->>'targetKind' = 'CHAT'
            WHEN 'UPSERT_PROTECTED_CONFIGURATION' THEN item->>'sourceTable' = 'matter_codex_agent_roles'
            WHEN 'UPSERT_SESSION' THEN item->>'sourceTable' = 'matter_codex_agent_sessions' AND item->>'targetKind' = 'SESSION'
            WHEN 'UPSERT_TURN' THEN item->>'sourceTable' = 'matter_codex_agent_session_turns' AND item->>'targetKind' = 'TURN'
            WHEN 'UPSERT_TURN_ATTEMPT' THEN item->>'sourceTable' = 'matter_codex_agent_session_turns' AND item->>'targetKind' = 'TURN_ATTEMPT'
            WHEN 'UPSERT_PROCESS_RUN' THEN item->>'sourceTable' = 'matter_codex_process_runs' AND item->>'targetKind' = 'PROCESS_RUN'
            WHEN 'UPSERT_SCHEDULE' THEN item->>'sourceTable' = 'matter_codex_automation_schedules' AND item->>'targetKind' = 'SCHEDULE'
            ELSE false END THEN
            RAISE EXCEPTION 'legacy materialization operation registry mismatch' USING ERRCODE = '22023';
        END IF;

        IF operation = 'UPSERT_TURN_ATTEMPT' THEN
            SELECT * INTO STRICT project FROM control_plane.resources AS resource
            WHERE resource.id = (item->>'projectTargetId')::uuid AND resource.kind = 'PROJECT'
              AND resource.project_id = resource.id AND resource.state = 'ACTIVE';
            INSERT INTO control_plane.turn_attempts (
                turn_id, attempt, workload_id, authority_generation, state,
                input_sha256, lease_fence, started_at,
                runtime_revision_id, runtime_revision_version
            ) VALUES (
                (item#>>'{resource,spec,turnId}')::uuid,
                (item#>>'{resource,spec,attempt}')::integer,
                left('legacy-cutover-' || cutover.plan_id, 128), 1, 'QUEUED',
                item#>>'{resource,spec,inputSha256}', 1, transaction_timestamp(),
                (item#>>'{resource,spec,runtimeRevisionId}')::uuid,
                (item#>>'{resource,spec,runtimeRevisionVersion}')::bigint
            ) ON CONFLICT (turn_id, attempt) DO NOTHING;
            IF NOT EXISTS (SELECT 1 FROM control_plane.turn_attempts AS attempt
                WHERE attempt.turn_id = (item#>>'{resource,spec,turnId}')::uuid
                  AND attempt.attempt = (item#>>'{resource,spec,attempt}')::integer
                  AND attempt.state = 'QUEUED'
                  AND attempt.input_sha256 = item#>>'{resource,spec,inputSha256}'
                  AND attempt.workload_id = left('legacy-cutover-' || cutover.plan_id, 128)
                  AND attempt.authority_generation = 1 AND attempt.lease_fence = 1
                  AND attempt.runtime_revision_id = (item#>>'{resource,spec,runtimeRevisionId}')::uuid
                  AND attempt.runtime_revision_version = (item#>>'{resource,spec,runtimeRevisionVersion}')::bigint) THEN
                RAISE EXCEPTION 'legacy turn attempt readback mismatch' USING ERRCODE = '55000';
            END IF;
            INSERT INTO control_plane.audit_events (
                id, organization_id, project_id, actor_id, action, resource_id,
                resource_kind, resource_version, outcome, correlation_id,
                policy_revision, occurred_at
            ) VALUES (
                md5('legacy-cutover-audit:' || cutover.plan_id || ':' || item->>'targetId')::uuid,
                project.organization_id, project.project_id, project.owner_actor_id,
                'legacy:materialized', (item#>>'{resource,spec,turnId}')::uuid,
                'TURN_ATTEMPT', 1, 'succeeded', md5('legacy-cutover:' || cutover.plan_id)::uuid,
                1, transaction_timestamp()
            ) ON CONFLICT (id) DO NOTHING;
            CONTINUE;
        END IF;

        IF operation = 'UPSERT_PROJECT' THEN
            SELECT * INTO STRICT history FROM control_plane.protected_resource_history AS evidence
            WHERE evidence.resource_id = (item->>'authorityTargetId')::uuid
              AND evidence.resource_version = (item->>'authorityVersion')::bigint
              AND evidence.snapshot_sha256 = item->>'authoritySha256';
            project.organization_id := history.organization_id;
            project.project_id := (item->>'targetId')::uuid;
            project.owner_actor_id := history.owner_actor_id;
        ELSE
            SELECT * INTO STRICT project FROM control_plane.resources AS resource
            WHERE resource.id = (item->>'projectTargetId')::uuid AND resource.kind = 'PROJECT'
              AND resource.project_id = resource.id AND resource.state = 'ACTIVE';
        END IF;

        IF operation = 'UPSERT_PROTECTED_CONFIGURATION' THEN
            SELECT * INTO STRICT history FROM control_plane.protected_resource_history AS evidence
            WHERE evidence.organization_id = project.organization_id
              AND evidence.project_id = project.project_id
              AND evidence.owner_actor_id = project.owner_actor_id
              AND evidence.resource_id = (item->>'authorityTargetId')::uuid
              AND evidence.resource_version = (item->>'authorityVersion')::bigint
              AND evidence.snapshot_sha256 = item->>'authoritySha256'
              AND evidence.resource_kind = item->>'targetKind';
            UPDATE control_plane.resources AS resource SET
                parent_id = nullif(history.snapshot->>'parentId', '')::uuid,
                name = history.snapshot->>'name', state = history.snapshot->>'state',
                version = history.resource_version, spec = history.snapshot->'spec',
                updated_at = transaction_timestamp()
            WHERE resource.id = history.resource_id
              AND resource.organization_id = history.organization_id
              AND resource.project_id = history.project_id
              AND resource.owner_actor_id = history.owner_actor_id
              AND resource.kind = history.resource_kind;
            IF NOT FOUND THEN RAISE EXCEPTION 'protected configuration authority is missing' USING ERRCODE = '55000'; END IF;
            INSERT INTO control_plane.audit_events (
                id, organization_id, project_id, actor_id, action, resource_id,
                resource_kind, resource_version, outcome, correlation_id,
                policy_revision, occurred_at
            ) VALUES (
                md5('legacy-cutover-audit:' || cutover.plan_id || ':' || history.resource_id::text)::uuid,
                history.organization_id, history.project_id, history.owner_actor_id,
                'legacy:materialized', history.resource_id, history.resource_kind,
                history.resource_version, 'succeeded', md5('legacy-cutover:' || cutover.plan_id)::uuid,
                1, transaction_timestamp()
            ) ON CONFLICT (id) DO NOTHING;
            CONTINUE;
        END IF;

        IF operation = 'UPSERT_SESSION' AND NOT EXISTS (
            SELECT 1 FROM control_plane.resources AS agent
            JOIN control_plane.resources AS chat ON chat.id = (item#>>'{resource,spec,conversationId}')::uuid
            WHERE agent.id = (item#>>'{resource,spec,agentId}')::uuid
              AND agent.kind = 'AGENT' AND agent.state = 'ACTIVE'
              AND chat.kind = 'CHAT' AND chat.state = 'ACTIVE'
              AND agent.organization_id = project.organization_id AND agent.project_id = project.project_id
              AND agent.owner_actor_id = project.owner_actor_id
              AND chat.organization_id = project.organization_id AND chat.project_id = project.project_id
              AND chat.owner_actor_id = project.owner_actor_id
        ) THEN
            RAISE EXCEPTION 'legacy session authority is stale' USING ERRCODE = '55000';
        END IF;
        IF operation = 'UPSERT_TURN' AND NOT EXISTS (
            SELECT 1
            FROM control_plane.resources AS revision
            JOIN control_plane.resources AS artifact
              ON artifact.id = (item#>>'{resource,spec,promptArtifactId}')::uuid
            WHERE revision.id = (item#>>'{resource,spec,runtimeRevisionId}')::uuid
              AND revision.kind = 'RUNTIME_REVISION' AND revision.state = 'ACTIVE'
              AND artifact.kind = 'ARTIFACT' AND artifact.state = 'ACTIVE' AND artifact.version > 0
              AND revision.organization_id = project.organization_id AND revision.project_id = project.project_id
              AND revision.owner_actor_id = project.owner_actor_id
              AND artifact.organization_id = project.organization_id AND artifact.project_id = project.project_id
              AND artifact.owner_actor_id = project.owner_actor_id
              AND artifact.spec->>'sha256' = item#>>'{resource,spec,effectiveInputSha256}'
              AND artifact.spec->>'scanStatus' = 'CLEAN'
              AND (artifact.spec->>'scanPolicyRevision')::bigint > 0
              AND artifact.spec->>'scanEvidenceSha256' ~ '^[a-f0-9]{64}$'
              AND artifact.spec->>'storageRef' ~ '^s3://[^/?#]+/[^?#]+[?]versionId=[^&#]+$'
              AND EXISTS (
                  SELECT 1 FROM jsonb_array_elements(revision.spec->'components') AS component(value)
                  WHERE component.value->>'kind' = 'ARTIFACT'
                    AND component.value->>'resourceId' = artifact.id::text
                    AND (component.value->>'version')::bigint = artifact.version
                    AND component.value->>'projectionSha256' ~ '^[a-f0-9]{64}$'
              )
        ) THEN
            RAISE EXCEPTION 'legacy turn runtime or artifact authority is stale' USING ERRCODE = '55000';
        END IF;

        INSERT INTO control_plane.resources (
            id, organization_id, project_id, parent_id, owner_actor_id, kind,
            name, state, version, spec, schedule_next_run_at, created_at, updated_at
        ) VALUES (
            (item->>'targetId')::uuid, project.organization_id, project.project_id,
            nullif(item#>>'{resource,parentId}', '')::uuid, project.owner_actor_id,
            item->>'targetKind', item#>>'{resource,name}', item#>>'{resource,state}',
            (item#>>'{resource,version}')::bigint, item#>'{resource,spec}',
            CASE WHEN item->>'targetKind' = 'SCHEDULE'
                 THEN (item#>>'{resource,spec,nextRunAt}')::timestamptz ELSE NULL END,
            transaction_timestamp(), transaction_timestamp()
        ) ON CONFLICT (id) DO NOTHING;
        SELECT * INTO STRICT stored FROM control_plane.resources AS resource
        WHERE resource.id = (item->>'targetId')::uuid;
        IF stored.organization_id <> project.organization_id OR stored.project_id <> project.project_id
           OR stored.owner_actor_id <> project.owner_actor_id OR stored.kind <> item->>'targetKind'
           OR stored.parent_id IS DISTINCT FROM nullif(item#>>'{resource,parentId}', '')::uuid
           OR stored.name <> item#>>'{resource,name}' OR stored.state <> item#>>'{resource,state}'
           OR stored.version <> (item#>>'{resource,version}')::bigint
           OR stored.spec <> item#>'{resource,spec}' THEN
            RAISE EXCEPTION 'legacy materialization readback mismatch' USING ERRCODE = '55000';
        END IF;
        INSERT INTO control_plane.audit_events (
            id, organization_id, project_id, actor_id, action, resource_id,
            resource_kind, resource_version, outcome, correlation_id,
            policy_revision, occurred_at
        ) VALUES (
            md5('legacy-cutover-audit:' || cutover.plan_id || ':' || stored.id::text)::uuid,
            stored.organization_id, stored.project_id, stored.owner_actor_id,
            'legacy:materialized', stored.id, stored.kind, stored.version, 'succeeded',
            md5('legacy-cutover:' || cutover.plan_id)::uuid, 1, transaction_timestamp()
        ) ON CONFLICT (id) DO NOTHING;
        IF operation = 'UPSERT_PROCESS_RUN' THEN
            provenance := item->'processProvenance';
            IF jsonb_typeof(provenance) <> 'object'
               OR provenance->>'rootActorSourceRef' = ''
               OR stored.spec->>'rootInitiatorActorId' <> stored.owner_actor_id::text
               OR NOT EXISTS (
                   SELECT 1 FROM control_plane.resources AS revision
                   WHERE revision.id = (stored.spec->>'runtimeRevisionId')::uuid
                     AND revision.kind = 'RUNTIME_REVISION' AND revision.state = 'ACTIVE'
                     AND revision.organization_id = stored.organization_id
                     AND revision.project_id = stored.project_id
                     AND revision.owner_actor_id = stored.owner_actor_id
                     AND (revision.spec->>'authorityPolicyRevision')::bigint =
                         (provenance->>'policyRevision')::bigint
                     AND revision.spec->>'authorityPolicySha256' = provenance->>'policySha256'
               ) THEN
                RAISE EXCEPTION 'legacy process provenance is missing' USING ERRCODE = '22023';
            END IF;
            INSERT INTO control_plane.legacy_data_cutover_provenance (
                plan_id, process_run_id, source_table, source_id, source_digest,
                target_actor_id, provenance, created_at
            ) VALUES (
                cutover.plan_id, stored.id, item->>'sourceTable', (item->>'sourceId')::bigint,
                item->>'sourceDigest', stored.owner_actor_id, provenance, transaction_timestamp()
            ) ON CONFLICT DO NOTHING;
            IF NOT EXISTS (SELECT 1 FROM control_plane.legacy_data_cutover_provenance AS receipt
                WHERE receipt.plan_id = cutover.plan_id AND receipt.process_run_id = stored.id
                  AND receipt.source_table = item->>'sourceTable'
                  AND receipt.source_id = (item->>'sourceId')::bigint
                  AND receipt.source_digest = item->>'sourceDigest'
                  AND receipt.target_actor_id = stored.owner_actor_id
                  AND receipt.provenance = provenance) THEN
                RAISE EXCEPTION 'legacy process provenance readback mismatch' USING ERRCODE = '55000';
            END IF;
        END IF;
    END LOOP;
    PERFORM control_plane.complete_legacy_data_cutover_materialization(cutover.plan_id);
    RETURN QUERY SELECT receipt.plan_id, receipt.plan_sha256, receipt.source_sha256,
        receipt.target_sha256, receipt.backup_sha256, receipt.manifest_sha256,
        receipt.materialization_sha256, receipt.materialization_count, receipt.state, true
    FROM control_plane.legacy_data_cutovers AS receipt WHERE receipt.plan_id = cutover.plan_id;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.materialize_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) OWNER TO control_plane_legacy_materializer;

UPDATE control_plane.schema_state
SET version = 20260807019601, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'migration 20260807019601 is forward-only: immutable cutover receipts cannot be relaxed';
END $$;
-- +goose StatementEnd
