-- +goose Up
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'control_plane_migration') THEN
        CREATE ROLE control_plane_migration
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'control_plane_legacy_materializer') THEN
        CREATE ROLE control_plane_legacy_materializer
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
END
$roles$;
ALTER ROLE control_plane_migration
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE control_plane_legacy_materializer
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
GRANT control_plane_legacy_materializer TO control_plane_owner;

SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.legacy_data_cutovers (
    plan_id text PRIMARY KEY CHECK (plan_id ~ '^[a-z0-9][a-z0-9._-]{15,127}$'),
    plan_sha256 text NOT NULL CHECK (plan_sha256 ~ '^[a-f0-9]{64}$'),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    target_sha256 text NOT NULL CHECK (target_sha256 ~ '^[a-f0-9]{64}$'),
    backup_sha256 text NOT NULL CHECK (backup_sha256 ~ '^[a-f0-9]{64}$'),
    manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[a-f0-9]{64}$'),
    materialization_sha256 text NOT NULL CHECK (materialization_sha256 ~ '^[a-f0-9]{64}$'),
    materialization_count bigint NOT NULL CHECK (materialization_count BETWEEN 0 AND 100000),
    materialization_plan text NOT NULL CHECK (
        octet_length(materialization_plan) <= 1048576
        AND jsonb_typeof(materialization_plan::jsonb) = 'array'
        AND materialization_count = jsonb_array_length(materialization_plan::jsonb)
        AND materialization_sha256 = encode(
            control_plane_extensions.digest(convert_to(materialization_plan, 'UTF8'), 'sha256'),
            'hex'
        )
    ),
    mapping_counts jsonb NOT NULL CHECK (
        jsonb_typeof(mapping_counts) = 'object'
        AND octet_length(mapping_counts::text) <= 16384
    ),
    state text NOT NULL CHECK (state IN ('PREPARED', 'COMMITTED', 'ABORTED')),
    prepared_at timestamptz NOT NULL,
    restore_verified_at timestamptz,
    committed_at timestamptz,
    aborted_at timestamptz,
    materialization_running boolean NOT NULL DEFAULT false,
    CHECK ((state = 'COMMITTED') = (committed_at IS NOT NULL)),
    CHECK (state <> 'COMMITTED' OR restore_verified_at IS NOT NULL),
    CHECK ((state = 'ABORTED') = (aborted_at IS NOT NULL)),
    CHECK (state = 'PREPARED' OR NOT materialization_running)
);
CREATE UNIQUE INDEX legacy_data_cutovers_one_winner_uidx
    ON control_plane.legacy_data_cutovers ((true))
    WHERE state = 'COMMITTED';
ALTER TABLE control_plane.legacy_data_cutovers OWNER TO control_plane_owner;
REVOKE ALL ON control_plane.legacy_data_cutovers FROM PUBLIC;
GRANT USAGE ON SCHEMA control_plane TO control_plane_migration;
GRANT USAGE ON SCHEMA control_plane_extensions TO control_plane_migration;
GRANT EXECUTE ON FUNCTION control_plane_extensions.digest(bytea, text)
    TO control_plane_migration;
GRANT SELECT, INSERT, UPDATE ON control_plane.legacy_data_cutovers TO control_plane_migration;
GRANT USAGE ON SCHEMA control_plane TO control_plane_legacy_materializer;
GRANT USAGE ON SCHEMA control_plane_extensions TO control_plane_legacy_materializer;
GRANT EXECUTE ON FUNCTION control_plane_extensions.digest(bytea, text)
    TO control_plane_legacy_materializer;
GRANT SELECT, UPDATE ON control_plane.legacy_data_cutovers TO control_plane_legacy_materializer;
GRANT SELECT, INSERT ON control_plane.resources, control_plane.audit_events
    TO control_plane_legacy_materializer;
GRANT SELECT ON control_plane.protected_resource_history
    TO control_plane_legacy_materializer;

CREATE FUNCTION control_plane.lock_legacy_cutover_resources()
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $$
BEGIN
    LOCK TABLE control_plane.resources, control_plane.turn_attempts,
        control_plane.runtime_executions, control_plane.runtime_derived_resources,
        control_plane.protected_resource_history IN SHARE MODE;
END
$$;
ALTER FUNCTION control_plane.lock_legacy_cutover_resources() OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.lock_legacy_cutover_resources() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.lock_legacy_cutover_resources() TO control_plane_migration;

-- Миграционная identity получает только readback target state. Запись в
-- resources, audit, event и runtime tables намеренно отсутствует.
CREATE POLICY resources_legacy_migration_read
    ON control_plane.resources
    FOR SELECT TO control_plane_migration
    USING (true);
GRANT SELECT ON control_plane.resources TO control_plane_migration;
CREATE POLICY turn_attempts_legacy_migration_read
    ON control_plane.turn_attempts
    FOR SELECT TO control_plane_migration
    USING (
        EXISTS (
            SELECT 1 FROM control_plane.resources AS turn
            WHERE turn.id = turn_attempts.turn_id AND turn.kind = 'TURN'
        )
    );
GRANT SELECT ON control_plane.turn_attempts TO control_plane_migration;
CREATE POLICY runtime_executions_legacy_migration_read
    ON control_plane.runtime_executions
    FOR SELECT TO control_plane_migration
    USING (
        EXISTS (
            SELECT 1 FROM control_plane.resources AS process
            WHERE process.id = runtime_executions.process_id AND process.kind = 'PROCESS_RUN'
        )
    );
GRANT SELECT ON control_plane.runtime_executions TO control_plane_migration;
CREATE POLICY runtime_derived_resources_legacy_migration_read
    ON control_plane.runtime_derived_resources
    FOR SELECT TO control_plane_migration
    USING (true);
GRANT SELECT ON control_plane.runtime_derived_resources TO control_plane_migration;
CREATE POLICY protected_resource_history_legacy_migration_read
    ON control_plane.protected_resource_history
    FOR SELECT TO control_plane_migration
    USING (true);
GRANT SELECT ON control_plane.protected_resource_history TO control_plane_migration;

-- Только эта owner-controlled capability материализует закрытый набор
-- Schedule-команд. Caller не получает произвольный DML к business tables.
CREATE POLICY resources_legacy_materialization_owner
    ON control_plane.resources
    FOR ALL TO control_plane_legacy_materializer
    USING (
        pg_has_role(session_user, 'control_plane_migration', 'member')
        AND EXISTS (
            SELECT 1 FROM control_plane.legacy_data_cutovers AS cutover
            WHERE cutover.plan_id = current_setting(
                'mattercodex.legacy_materialization_plan_id', true
            )
              AND (
                  cutover.state = 'COMMITTED'
                  OR cutover.state = 'PREPARED' AND cutover.materialization_running
              )
        )
    )
    WITH CHECK (
        pg_has_role(session_user, 'control_plane_migration', 'member')
        AND EXISTS (
            SELECT 1 FROM control_plane.legacy_data_cutovers AS cutover
            WHERE cutover.plan_id = current_setting(
                'mattercodex.legacy_materialization_plan_id', true
            )
              AND (
                  cutover.state = 'COMMITTED'
                  OR cutover.state = 'PREPARED' AND cutover.materialization_running
              )
        )
    );
CREATE POLICY audit_events_legacy_materialization_owner
    ON control_plane.audit_events
    FOR ALL TO control_plane_legacy_materializer
    USING (
        pg_has_role(session_user, 'control_plane_migration', 'member')
        AND EXISTS (
            SELECT 1 FROM control_plane.legacy_data_cutovers AS cutover
            WHERE cutover.plan_id = current_setting(
                'mattercodex.legacy_materialization_plan_id', true
            )
              AND (
                  cutover.state = 'COMMITTED'
                  OR cutover.state = 'PREPARED' AND cutover.materialization_running
              )
        )
    )
    WITH CHECK (
        pg_has_role(session_user, 'control_plane_migration', 'member')
        AND EXISTS (
            SELECT 1 FROM control_plane.legacy_data_cutovers AS cutover
            WHERE cutover.plan_id = current_setting(
                'mattercodex.legacy_materialization_plan_id', true
            )
              AND (
                  cutover.state = 'COMMITTED'
                  OR cutover.state = 'PREPARED' AND cutover.materialization_running
              )
        )
    );
CREATE POLICY protected_history_legacy_materialization_owner
    ON control_plane.protected_resource_history
    FOR SELECT TO control_plane_legacy_materializer
    USING (
        pg_has_role(session_user, 'control_plane_migration', 'member')
        AND EXISTS (
            SELECT 1 FROM control_plane.legacy_data_cutovers AS cutover
            WHERE cutover.plan_id = current_setting(
                'mattercodex.legacy_materialization_plan_id', true
            )
              AND (
                  cutover.state = 'COMMITTED'
                  OR cutover.state = 'PREPARED' AND cutover.materialization_running
              )
        )
    );

-- +goose StatementBegin
CREATE FUNCTION control_plane.materialize_legacy_data_cutover(
    requested_plan_id text,
    requested_plan_sha256 text,
    requested_source_sha256 text,
    requested_target_sha256 text,
    requested_backup_sha256 text,
    requested_manifest_sha256 text,
    requested_materialization_sha256 text,
    requested_materialization_count bigint
) RETURNS TABLE (
    plan_id text,
    plan_sha256 text,
    source_sha256 text,
    target_sha256 text,
    backup_sha256 text,
    manifest_sha256 text,
    materialization_sha256 text,
    materialization_count bigint,
    state text,
    restore_verified boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
SET row_security = on
AS $function$
DECLARE
    cutover control_plane.legacy_data_cutovers%ROWTYPE;
    item record;
    command jsonb;
    project control_plane.resources%ROWTYPE;
    agent control_plane.resources%ROWTYPE;
    chat control_plane.resources%ROWTYPE;
    instruction control_plane.resources%ROWTYPE;
    provider_pool control_plane.resources%ROWTYPE;
    runtime_recipe control_plane.resources%ROWTYPE;
    assignment control_plane.resources%ROWTYPE;
    prompt_artifact control_plane.resources%ROWTYPE;
    existing_schedule control_plane.resources%ROWTYPE;
    expected_spec jsonb;
    expected_target_id uuid;
    expected_agent_sha text;
    expected_assignment_sha text;
    audit_id uuid;
    correlation_id uuid;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_migration', 'member') THEN
        RAISE EXCEPTION 'legacy materialization caller is invalid' USING ERRCODE = '28000';
    END IF;

    SELECT * INTO STRICT cutover
    FROM control_plane.legacy_data_cutovers AS stored
    WHERE stored.plan_id = requested_plan_id
    FOR UPDATE;

    IF cutover.plan_sha256 <> requested_plan_sha256
       OR cutover.source_sha256 <> requested_source_sha256
       OR cutover.target_sha256 <> requested_target_sha256
       OR cutover.backup_sha256 <> requested_backup_sha256
       OR cutover.manifest_sha256 <> requested_manifest_sha256
       OR cutover.materialization_sha256 <> requested_materialization_sha256
       OR cutover.materialization_count <> requested_materialization_count
       OR cutover.restore_verified_at IS NULL
       OR cutover.state NOT IN ('PREPARED', 'COMMITTED') THEN
        RAISE EXCEPTION 'legacy materialization receipt is invalid' USING ERRCODE = '55000';
    END IF;

    IF cutover.state = 'PREPARED' THEN
        UPDATE control_plane.legacy_data_cutovers AS stored
        SET materialization_running = true
        WHERE stored.plan_id = cutover.plan_id;
    END IF;
    PERFORM set_config('mattercodex.legacy_materialization_plan_id', cutover.plan_id, true);

    FOR item IN
        SELECT value, ordinality
        FROM jsonb_array_elements(cutover.materialization_plan::jsonb)
            WITH ORDINALITY AS planned(value, ordinality)
        ORDER BY ordinality
    LOOP
        command := item.value;
        IF jsonb_typeof(command) <> 'object'
           OR (SELECT count(*) FROM jsonb_object_keys(command)) <> 17
           OR NOT command ?& ARRAY[
               'operation', 'sourceTable', 'sourceId', 'sourcePublicId',
               'sourceRevision', 'sourceDigest', 'targetId', 'projectSlug',
               'agentStableKey', 'chatStableKey', 'promptSha256', 'localTime',
               'timezone', 'nextRunAt', 'playbookKey', 'promptVersion',
               'callbackContractVersion'
           ]
           OR command->>'operation' <> 'UPSERT_SCHEDULE'
           OR command->>'sourceTable' <> 'matter_codex_automation_schedules'
           OR (command->>'sourceId')::bigint < 1
           OR (command->>'sourceRevision')::bigint < 1
           OR command->>'sourcePublicId' !~ '^schedule-[a-f0-9]{32}$'
           OR command->>'sourceDigest' !~ '^[a-f0-9]{64}$'
           OR command->>'promptSha256' !~ '^[a-f0-9]{64}$'
           OR command->>'localTime' !~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
           OR NOT EXISTS (
               SELECT 1 FROM pg_catalog.pg_timezone_names AS zone
               WHERE zone.name = command->>'timezone'
           )
           OR (command->>'nextRunAt')::timestamptz IS NULL
           OR length(command->>'playbookKey') NOT BETWEEN 1 AND 512
           OR command->>'playbookKey' <> btrim(command->>'playbookKey')
           OR command->>'playbookKey' ~ '[[:cntrl:]]'
           OR length(command->>'promptVersion') NOT BETWEEN 1 AND 512
           OR command->>'promptVersion' <> btrim(command->>'promptVersion')
           OR command->>'promptVersion' ~ '[[:cntrl:]]'
           OR length(command->>'callbackContractVersion') NOT BETWEEN 1 AND 512
           OR command->>'callbackContractVersion' <> btrim(command->>'callbackContractVersion')
           OR command->>'callbackContractVersion' ~ '[[:cntrl:]]' THEN
            RAISE EXCEPTION 'legacy materialization command is invalid' USING ERRCODE = '22023';
        END IF;

        expected_target_id := md5(
            'mattercodex:legacy-schedule:' || command->>'sourcePublicId'
        )::uuid;
        IF (command->>'targetId')::uuid <> expected_target_id THEN
            RAISE EXCEPTION 'legacy materialization target identity is invalid' USING ERRCODE = '22023';
        END IF;

        SELECT candidate.* INTO STRICT project
        FROM control_plane.resources AS candidate
        WHERE candidate.kind = 'PROJECT'
          AND candidate.project_id = candidate.id
          AND candidate.state = 'ACTIVE'
          AND candidate.spec->>'slug' = command->>'projectSlug';
        SELECT candidate.* INTO STRICT agent
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = project.organization_id
          AND candidate.project_id = project.project_id
          AND candidate.owner_actor_id = project.owner_actor_id
          AND candidate.kind = 'AGENT'
          AND candidate.state = 'ACTIVE'
          AND candidate.spec->>'stableKey' = command->>'agentStableKey';
        SELECT candidate.* INTO STRICT chat
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = project.organization_id
          AND candidate.project_id = project.project_id
          AND candidate.owner_actor_id = project.owner_actor_id
          AND candidate.kind = 'CHAT'
          AND candidate.state = 'ACTIVE'
          AND candidate.spec->>'stableKey' = command->>'chatStableKey';

        SELECT history.snapshot_sha256 INTO STRICT expected_agent_sha
        FROM control_plane.protected_resource_history AS history
        WHERE history.organization_id = agent.organization_id
          AND history.project_id = agent.project_id
          AND history.resource_id = agent.id
          AND history.resource_version = agent.version
          AND history.resource_kind = 'AGENT'
          AND history.owner_actor_id = agent.owner_actor_id;

        SELECT candidate.* INTO STRICT instruction
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = agent.organization_id
          AND candidate.project_id = agent.project_id
          AND candidate.owner_actor_id = agent.owner_actor_id
          AND candidate.id = (agent.spec->>'instructionSetId')::uuid
          AND candidate.kind = 'INSTRUCTION_SET'
          AND candidate.state = 'ACTIVE'
          AND candidate.version = (agent.spec->>'instructionSetVersion')::bigint
          AND agent.spec->>'instructionSetSha256' ~ '^[a-f0-9]{64}$'
          AND candidate.spec->>'versionState' = 'PUBLISHED'
          AND candidate.spec->>'contentSha256' = command->>'promptSha256';
        IF NOT EXISTS (
            SELECT 1 FROM control_plane.protected_resource_history AS history
            WHERE history.organization_id = instruction.organization_id
              AND history.project_id = instruction.project_id
              AND history.resource_id = instruction.id
              AND history.resource_version = instruction.version
              AND history.snapshot_sha256 = agent.spec->>'instructionSetSha256'
        ) THEN
            RAISE EXCEPTION 'legacy materialization instruction binding is stale' USING ERRCODE = '55000';
        END IF;

        SELECT candidate.* INTO STRICT provider_pool
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = agent.organization_id
          AND candidate.project_id = agent.project_id
          AND candidate.owner_actor_id = agent.owner_actor_id
          AND candidate.id = (agent.spec->>'providerPoolId')::uuid
          AND candidate.kind = 'PROVIDER_POOL'
          AND candidate.state = 'ACTIVE'
          AND candidate.version = (agent.spec->>'providerPoolVersion')::bigint;
        IF NOT EXISTS (
            SELECT 1 FROM control_plane.protected_resource_history AS history
            WHERE history.organization_id = provider_pool.organization_id
              AND history.project_id = provider_pool.project_id
              AND history.resource_id = provider_pool.id
              AND history.resource_version = provider_pool.version
              AND history.snapshot_sha256 = agent.spec->>'providerPoolSha256'
        ) THEN
            RAISE EXCEPTION 'legacy materialization provider binding is stale' USING ERRCODE = '55000';
        END IF;

        SELECT candidate.* INTO STRICT runtime_recipe
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = agent.organization_id
          AND candidate.project_id = agent.project_id
          AND candidate.owner_actor_id = agent.owner_actor_id
          AND agent.spec->>'runtimeProfileRef' ~ '^control-plane://runtime-profile/[a-f0-9-]{36}$'
          AND candidate.id = substring(
              agent.spec->>'runtimeProfileRef'
              FROM length('control-plane://runtime-profile/') + 1
          )::uuid
          AND candidate.kind = 'ROLE_IMAGE_RECIPE'
          AND candidate.state = 'ACTIVE'
          AND candidate.version = (agent.spec->>'runtimeProfileVersion')::bigint
          AND agent.spec->>'runtimeProfileSha256' ~ '^[a-f0-9]{64}$';

        SELECT candidate.* INTO STRICT assignment
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = agent.organization_id
          AND candidate.project_id = agent.project_id
          AND candidate.owner_actor_id = agent.owner_actor_id
          AND candidate.kind = 'AGENT_ASSIGNMENT'
          AND candidate.state = 'ACTIVE'
          AND candidate.spec->>'agentId' = agent.id::text
          AND (candidate.spec->>'agentVersion')::bigint = agent.version
          AND candidate.spec->>'agentSha256' = expected_agent_sha
          AND candidate.spec->>'workspaceId' = project.id::text
          AND candidate.spec->>'rootActorId' = project.owner_actor_id::text
          AND candidate.spec->>'roomId' = chat.id::text;
        SELECT history.snapshot_sha256 INTO STRICT expected_assignment_sha
        FROM control_plane.protected_resource_history AS history
        WHERE history.organization_id = assignment.organization_id
          AND history.project_id = assignment.project_id
          AND history.resource_id = assignment.id
          AND history.resource_version = assignment.version
          AND history.resource_kind = 'AGENT_ASSIGNMENT'
          AND history.owner_actor_id = assignment.owner_actor_id;

        SELECT candidate.* INTO STRICT prompt_artifact
        FROM control_plane.resources AS candidate
        WHERE candidate.organization_id = project.organization_id
          AND candidate.project_id = project.project_id
          AND candidate.owner_actor_id = project.owner_actor_id
          AND candidate.kind = 'ARTIFACT'
          AND candidate.state = 'ACTIVE'
          AND candidate.version > 0
          AND candidate.id = (instruction.spec->>'contentArtifactId')::uuid
          AND candidate.version = (instruction.spec->>'contentArtifactVersion')::bigint
          AND candidate.spec->>'direction' = 'INPUT'
          AND candidate.spec->>'kind' ~ '^[a-z][a-z0-9]*([-_][a-z0-9]+)*$'
          AND candidate.spec->>'mediaType' = 'text/markdown'
          AND candidate.spec->>'sha256' = command->>'promptSha256'
          AND candidate.spec->>'scanStatus' = 'CLEAN'
          AND (candidate.spec->>'scanPolicyRevision')::bigint > 0
          AND candidate.spec->>'scanEvidenceSha256' ~ '^[a-f0-9]{64}$'
          AND (candidate.spec->>'sizeBytes')::bigint BETWEEN 1 AND 10737418240
          AND length(candidate.spec->>'retentionPolicyRef') BETWEEN 1 AND 512
          AND candidate.spec->>'retentionPolicyRef' = btrim(candidate.spec->>'retentionPolicyRef')
          AND candidate.spec->>'retentionPolicyRef' !~ '[[:cntrl:]]'
          AND candidate.spec->>'scannerWorkloadId' ~ '^[a-z][a-z0-9]*([-_][a-z0-9]+)*$'
          AND (candidate.spec->>'scannedAt')::timestamptz IS NOT NULL
          AND length(candidate.spec->>'storageRef') BETWEEN 1 AND 512
          AND candidate.spec->>'storageRef' ~ '^s3://[^/?#]+/[^?#]+[?]versionId=[^&#]+$';

        expected_spec := jsonb_build_object(
            'targetResourceId', agent.id::text,
            'targetKind', 'AGENT',
            'targetVersion', agent.version,
            'effectiveInputSha256', command->>'sourceDigest',
            'cron', split_part(command->>'localTime', ':', 2) || ' ' ||
                split_part(command->>'localTime', ':', 1) || ' * * *',
            'timezone', command->>'timezone',
            'calendar', 'GREGORIAN',
            'overlapPolicy', 'FORBID',
            'misfirePolicy', 'RUN_ONCE',
            'misfireGrace', 0,
            'nextRunAt', command->>'nextRunAt',
            'deliveryPolicy', 'EXACTLY_ONCE_EFFECT',
            'maximumAttempts', 3,
            'initialBackoff', 5000000000,
            'maximumBackoff', 60000000000,
            'deadLetterAfter', 86400000000000,
            'sessionPolicy', 'NEW',
            'roomId', chat.id::text,
            'notificationPolicy', 'ON_ACTION_OR_FAILURE',
            'maximumExecutionDuration', 5400000000000,
            'coalesce', true,
            'targetType', 'PLAYBOOK',
            'playbookRef', command->>'playbookKey',
            'playbookVersion', 1,
            'promptArtifactId', prompt_artifact.id::text,
            'ownership', jsonb_build_object(
                'managedBy', 'UI',
                'sourceRef', 'control-plane://legacy-schedule/' || command->>'sourcePublicId',
                'sourceRevision', (command->>'sourceRevision')::bigint,
                'sourceSha256', command->>'sourceDigest'
            ),
            'agentId', agent.id::text,
            'agentVersion', agent.version,
            'agentSha256', expected_agent_sha,
            'instructionSetId', instruction.id::text,
            'instructionSetVersion', instruction.version,
            'instructionSetSha256', agent.spec->>'instructionSetSha256',
            'runtimeSelectionRef', agent.spec->>'runtimeProfileRef',
            'runtimeSelectionVersion', (agent.spec->>'runtimeProfileVersion')::bigint,
            'runtimeSelectionSha256', agent.spec->>'runtimeProfileSha256',
            'providerPoolId', provider_pool.id::text,
            'providerPoolVersion', provider_pool.version,
            'providerPoolSha256', agent.spec->>'providerPoolSha256',
            'agentAssignmentId', assignment.id::text,
            'agentAssignmentVersion', assignment.version,
            'agentAssignmentSha256', expected_assignment_sha
        );

        INSERT INTO control_plane.resources (
            id, organization_id, project_id, parent_id, owner_actor_id,
            kind, name, state, version, spec, schedule_next_run_at,
            created_at, updated_at
        ) VALUES (
            expected_target_id, project.organization_id, project.project_id,
            project.id, project.owner_actor_id, 'SCHEDULE',
            left('Legacy schedule ' || command->>'sourcePublicId', 160),
            'ACTIVE', 1, expected_spec, (command->>'nextRunAt')::timestamptz,
            transaction_timestamp(), transaction_timestamp()
        ) ON CONFLICT (id) DO NOTHING;

        SELECT candidate.* INTO STRICT existing_schedule
        FROM control_plane.resources AS candidate
        WHERE candidate.id = expected_target_id;
        IF existing_schedule.organization_id <> project.organization_id
           OR existing_schedule.project_id <> project.project_id
           OR existing_schedule.owner_actor_id <> project.owner_actor_id
           OR existing_schedule.parent_id <> project.id
           OR existing_schedule.kind <> 'SCHEDULE'
           OR existing_schedule.name <> left(
               'Legacy schedule ' || command->>'sourcePublicId', 160
           )
           OR existing_schedule.state <> 'ACTIVE'
           OR existing_schedule.version <> 1
           OR existing_schedule.spec <> expected_spec
           OR existing_schedule.schedule_next_run_at <> (command->>'nextRunAt')::timestamptz THEN
            RAISE EXCEPTION 'legacy materialization schedule readback mismatch' USING ERRCODE = '55000';
        END IF;

        audit_id := md5('mattercodex:legacy-schedule-audit:' || expected_target_id::text)::uuid;
        correlation_id := md5('mattercodex:legacy-cutover:' || cutover.plan_id)::uuid;
        INSERT INTO control_plane.audit_events (
            id, organization_id, project_id, actor_id, action,
            resource_id, resource_kind, resource_version, outcome,
            correlation_id, policy_revision, occurred_at
        ) VALUES (
            audit_id, project.organization_id, project.project_id,
            project.owner_actor_id, 'legacy_schedule_materialized',
            expected_target_id, 'SCHEDULE', 1, 'succeeded',
            correlation_id, 1, transaction_timestamp()
        ) ON CONFLICT (id) DO NOTHING;
        IF NOT EXISTS (
            SELECT 1 FROM control_plane.audit_events AS audit
            WHERE audit.id = audit_id
              AND audit.organization_id = project.organization_id
              AND audit.project_id = project.project_id
              AND audit.actor_id = project.owner_actor_id
              AND audit.action = 'legacy_schedule_materialized'
              AND audit.resource_id = expected_target_id
              AND audit.resource_kind = 'SCHEDULE'
              AND audit.resource_version = 1
              AND audit.outcome = 'succeeded'
              AND audit.correlation_id = correlation_id
        ) THEN
            RAISE EXCEPTION 'legacy materialization audit readback mismatch' USING ERRCODE = '55000';
        END IF;
    END LOOP;

    IF cutover.state = 'PREPARED' THEN
        UPDATE control_plane.legacy_data_cutovers AS stored
        SET state = 'COMMITTED',
            committed_at = transaction_timestamp(),
            materialization_running = false
        WHERE stored.plan_id = cutover.plan_id
          AND stored.state = 'PREPARED'
          AND stored.materialization_running;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'legacy materialization commit lost its fence' USING ERRCODE = '40001';
        END IF;
    END IF;

    RETURN QUERY
    SELECT stored.plan_id, stored.plan_sha256, stored.source_sha256,
        stored.target_sha256, stored.backup_sha256, stored.manifest_sha256,
        stored.materialization_sha256, stored.materialization_count,
        stored.state, stored.restore_verified_at IS NOT NULL
    FROM control_plane.legacy_data_cutovers AS stored
    WHERE stored.plan_id = cutover.plan_id;
END
$function$;
-- +goose StatementEnd
ALTER FUNCTION control_plane.materialize_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) OWNER TO control_plane_legacy_materializer;
REVOKE ALL ON FUNCTION control_plane.materialize_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.materialize_legacy_data_cutover(
    text, text, text, text, text, text, text, bigint
) TO control_plane_migration;

UPDATE control_plane.schema_state
SET version = 20260807019600, migrated_at = clock_timestamp()
WHERE singleton = true;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 20260807019600 is forward-only: cutover receipt cannot be removed safely';
END
$$;
-- +goose StatementEnd
