-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.provider_accounts
    DROP CONSTRAINT provider_accounts_state_check,
    DROP CONSTRAINT provider_accounts_state_enabled_check,
    DROP CONSTRAINT provider_accounts_credential_lifecycle_check,
    ADD CONSTRAINT provider_accounts_state_check CHECK (state IN (
        'PENDING_AUTHORIZATION', 'AUTHORIZED', 'REAUTHORIZATION_REQUIRED',
        'REVOKED', 'DISABLED', 'DELETING', 'DELETED')),
    ADD CONSTRAINT provider_accounts_state_enabled_check CHECK (
        (state <> 'AUTHORIZED' OR enabled)
        AND (state NOT IN ('DISABLED', 'REVOKED', 'DELETING', 'DELETED') OR NOT enabled)),
    ADD CONSTRAINT provider_accounts_credential_lifecycle_check CHECK (
        (state <> 'DISABLED' OR current_credential_revision_id IS NOT NULL)
        AND (state NOT IN ('REVOKED', 'DELETED') OR current_credential_revision_id IS NULL));

ALTER TABLE control_plane.provider_authorization_attempts
    ADD COLUMN preparation_state text NOT NULL DEFAULT 'APPLIED'
        CHECK (preparation_state IN ('RESERVED', 'APPLIED', 'ABANDONED')),
    ADD COLUMN request_key text,
    ADD COLUMN request_digest text,
    ADD COLUMN original_account_version bigint,
    ADD COLUMN reserved_account_version bigint,
    ADD COLUMN reservation_deadline timestamptz,
    ADD COLUMN materializer_attempt_uid uuid,
    ADD COLUMN materializer_attempt_resource_version text,
    ADD CONSTRAINT provider_authorization_materializer_pins CHECK (
        (materializer_attempt_uid IS NULL AND materializer_attempt_resource_version IS NULL)
        OR (materializer_attempt_uid IS NOT NULL
            AND materializer_attempt_resource_version IS NOT NULL
            AND char_length(materializer_attempt_resource_version) BETWEEN 1 AND 128));

ALTER TABLE control_plane.provider_authorization_attempts
    ADD CONSTRAINT provider_authorization_reservation_pins CHECK (
        (request_key IS NULL AND request_digest IS NULL AND original_account_version IS NULL
            AND reserved_account_version IS NULL AND reservation_deadline IS NULL AND preparation_state IN ('APPLIED','ABANDONED'))
        OR (request_key IS NOT NULL AND request_digest IS NOT NULL
            AND original_account_version IS NOT NULL AND reserved_account_version IS NOT NULL
            AND char_length(request_key) BETWEEN 8 AND 128 AND request_digest ~ '^[a-f0-9]{64}$'
            AND original_account_version > 0 AND reserved_account_version = original_account_version + 1
            AND reservation_deadline IS NOT NULL));
CREATE UNIQUE INDEX provider_authorization_request_identity
    ON control_plane.provider_authorization_attempts (organization_id, provider_account_id, created_by, method, request_key)
    WHERE request_key IS NOT NULL;
CREATE UNIQUE INDEX provider_authorization_one_reservation
    ON control_plane.provider_authorization_attempts (provider_account_id)
    WHERE preparation_state = 'RESERVED';

CREATE TABLE control_plane.provider_account_deletion_intents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^pdel_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL UNIQUE REFERENCES control_plane.provider_accounts(id),
    requested_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    state text NOT NULL CHECK (state IN ('PENDING_BLOCKERS', 'CLEANUP_QUEUED', 'CLEANING', 'FAILED', 'DELETED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    safe_reason text NOT NULL DEFAULT '' CHECK (char_length(safe_reason) <= 128),
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    CHECK ((state = 'DELETED') = (completed_at IS NOT NULL))
);

CREATE TABLE control_plane.provider_account_verifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^pverify_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    account_version bigint NOT NULL CHECK (account_version > 0),
    provider_credential_revision_id uuid NOT NULL REFERENCES control_plane.provider_credential_revisions(id),
    requested_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    model_catalog_task_id uuid UNIQUE REFERENCES control_plane.provider_model_catalog_tasks(id),
    state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'VERIFIED', 'FAILED', 'STALE')),
    safe_reason text NOT NULL DEFAULT 'VERIFICATION_PENDING' CHECK (safe_reason IN (
        'VERIFICATION_PENDING', 'CREDENTIAL_REACHABILITY_VERIFIED',
        'CREDENTIAL_VERIFICATION_FAILED', 'VERIFICATION_SOURCE_CHANGED')),
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    deadline timestamptz NOT NULL DEFAULT clock_timestamp() + interval '2 minutes',
    completed_at timestamptz,
    CHECK ((state <> 'PENDING') = (completed_at IS NOT NULL))
);
CREATE UNIQUE INDEX provider_verification_one_pending
    ON control_plane.provider_account_verifications(provider_account_id) WHERE state = 'PENDING';

ALTER TABLE control_plane.provider_credential_cleanup_tasks
    ALTER COLUMN provider_credential_revision_id DROP NOT NULL,
    ALTER COLUMN secret_name DROP NOT NULL,
    ALTER COLUMN secret_uid DROP NOT NULL,
    ALTER COLUMN secret_resource_version DROP NOT NULL,
    ALTER COLUMN content_sha256 DROP NOT NULL,
    ADD COLUMN target_kind text NOT NULL DEFAULT 'CREDENTIAL',
    ADD COLUMN provider_authorization_attempt_id uuid REFERENCES control_plane.provider_authorization_attempts(id),
    ADD COLUMN materializer_attempt_ref text,
    ADD COLUMN materializer_attempt_uid uuid,
    ADD COLUMN materializer_attempt_resource_version text,
    ADD COLUMN predecessor_task_id uuid REFERENCES control_plane.provider_credential_cleanup_tasks(id),
    ADD COLUMN completion_descriptor jsonb,
    ADD CONSTRAINT provider_cleanup_target_kind CHECK (target_kind IN (
        'CREDENTIAL', 'AUTHORIZATION_METADATA', 'AUTHORIZATION_ATTEMPT', 'AUTHORIZATION_ABSENCE')),
    ADD CONSTRAINT provider_cleanup_target_descriptor CHECK (
        (target_kind = 'CREDENTIAL'
            AND provider_credential_revision_id IS NOT NULL
            AND secret_name IS NOT NULL AND secret_uid IS NOT NULL
            AND secret_resource_version IS NOT NULL AND content_sha256 IS NOT NULL
            AND provider_authorization_attempt_id IS NULL
            AND materializer_attempt_ref IS NULL AND materializer_attempt_uid IS NULL
            AND materializer_attempt_resource_version IS NULL)
        OR (target_kind <> 'CREDENTIAL'
            AND provider_credential_revision_id IS NULL
            AND secret_name IS NULL AND secret_uid IS NULL
            AND secret_resource_version IS NULL AND content_sha256 IS NULL
            AND provider_authorization_attempt_id IS NOT NULL
            AND materializer_attempt_ref IS NOT NULL
            AND materializer_attempt_ref ~ '^pmat_[a-f0-9]{32}$'
            AND ((target_kind = 'AUTHORIZATION_ATTEMPT'
                AND materializer_attempt_uid IS NOT NULL
                AND materializer_attempt_resource_version IS NOT NULL
                AND char_length(materializer_attempt_resource_version) BETWEEN 1 AND 128)
                OR (target_kind IN ('AUTHORIZATION_METADATA', 'AUTHORIZATION_ABSENCE')
                    AND materializer_attempt_uid IS NULL AND materializer_attempt_resource_version IS NULL))))
;

-- +goose StatementBegin
CREATE FUNCTION control_plane.provider_account_blockers(p_organization_id uuid, p_account_id uuid)
RETURNS TABLE(kind text, ref text, version bigint, name text, project_id uuid,
              resource_kind text, resource_id uuid, owner_id uuid, source_pin text)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path = '' AS $$
WITH account AS (
    SELECT a.* FROM control_plane.provider_accounts a
    WHERE a.organization_id = p_organization_id AND a.id = p_account_id
), configured_agents AS (
    SELECT a.*, policy.mode, config.digest || ':' || policy.digest AS source_pin
    FROM account
    JOIN control_plane.agents a ON a.organization_id = account.organization_id
    JOIN control_plane.agent_runtime_config_versions config ON config.id = a.current_runtime_config_id
    JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
    LEFT JOIN control_plane.projects project ON project.id = a.project_id AND project.organization_id = a.organization_id
    WHERE a.state <> 'ARCHIVED' AND (a.project_id IS NULL OR project.lifecycle = 'ACTIVE')
      AND EXISTS (SELECT 1 FROM jsonb_array_elements(policy.account_candidates) candidate
                  WHERE candidate->>'accountRef' = account.ref)
), bound_schedules AS (
    SELECT DISTINCT schedule.id, schedule.ref, schedule.version, schedule.name,
        schedule.project_id, schedule.created_by
    FROM account
    JOIN control_plane.schedules schedule ON schedule.organization_id = account.organization_id
    WHERE schedule.lifecycle_state = 'ACTIVE' AND schedule.enabled
      AND (EXISTS (SELECT 1 FROM configured_agents agent
                   WHERE schedule.target_type = 'AGENT' AND schedule.target_ref = agent.ref)
           OR EXISTS (
               SELECT 1 FROM control_plane.workflows workflow
               JOIN configured_agents agent ON agent.organization_id = workflow.organization_id
               WHERE schedule.target_type = 'WORKFLOW' AND schedule.target_ref = workflow.ref
                 AND workflow.organization_id = account.organization_id
                 AND (workflow.published_spec->>'CoordinatorAgentRef' = agent.ref
                      OR EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(workflow.published_spec->'Steps', '[]'::jsonb)) step
                                 WHERE step->>'AgentRef' = agent.ref))))
)
SELECT CASE WHEN a.mode = 'FIXED' THEN 'AGENT' ELSE 'PROVIDER_POOL' END,
       a.ref, a.version, a.name, a.project_id, 'AGENT', a.id, a.created_by, a.source_pin
FROM configured_agents a
UNION ALL
SELECT 'AUTOMATION', s.ref, s.version, s.name, s.project_id, 'SCHEDULE', s.id, s.created_by, s.version::text
FROM bound_schedules s
UNION ALL
SELECT 'ACTIVE_TURN', run.ref, run.version, run.title, run.project_id, 'RUN', run.id, run.initiated_by,
       string_agg(lease.ref || ':' || lease.generation::text, ',' ORDER BY lease.ref)
FROM account
JOIN control_plane.runtime_revisions revision ON revision.provider_account_id = account.id AND revision.organization_id = account.organization_id
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id = revision.id AND lease.organization_id = account.organization_id
JOIN control_plane.runs run ON run.id = lease.run_id AND run.organization_id = account.organization_id
WHERE lease.state = 'CLAIMED'
GROUP BY run.id
UNION ALL
SELECT DISTINCT 'QUEUED_TURN', run.ref, run.version, run.title, run.project_id, 'RUN', run.id, run.initiated_by,
       session.ref || ':' || run.version::text
FROM account
JOIN control_plane.sessions session ON session.provider_account_id = account.id AND session.organization_id = account.organization_id
JOIN control_plane.session_turns turn ON turn.session_id = session.id AND turn.organization_id = account.organization_id
JOIN control_plane.runs run ON run.id = turn.run_id AND run.organization_id = account.organization_id
WHERE turn.state = 'QUEUED'
UNION ALL
SELECT 'WARM_RUNTIME', agent.ref, runtime.version, agent.name, agent.project_id, 'AGENT', agent.id, agent.created_by,
       runtime.warm_instance_ref || ':' || runtime.version::text
FROM account
JOIN control_plane.sessions session ON session.provider_account_id = account.id AND session.organization_id = account.organization_id
JOIN control_plane.assistant_runtime runtime ON runtime.system_session_ref = session.ref AND runtime.organization_id = account.organization_id
JOIN control_plane.agents agent ON agent.id = runtime.agent_id AND agent.organization_id = account.organization_id
WHERE runtime.warm_instance_ref IS NOT NULL;
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX provider_cleanup_single_successor
    ON control_plane.provider_credential_cleanup_tasks (predecessor_task_id)
    WHERE predecessor_task_id IS NOT NULL AND target_kind <> 'CREDENTIAL';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_provider_credential_cleanup_snapshot()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(OLD.organization_id, OLD.provider_account_id, OLD.provider_credential_revision_id,
           OLD.secret_name, OLD.secret_uid, OLD.secret_resource_version, OLD.content_sha256,
           OLD.maximum_attempts, OLD.created_at, OLD.target_kind,
           OLD.provider_authorization_attempt_id, OLD.materializer_attempt_ref,
           OLD.materializer_attempt_uid, OLD.materializer_attempt_resource_version, OLD.predecessor_task_id)
       IS DISTINCT FROM
       ROW(NEW.organization_id, NEW.provider_account_id, NEW.provider_credential_revision_id,
           NEW.secret_name, NEW.secret_uid, NEW.secret_resource_version, NEW.content_sha256,
           NEW.maximum_attempts, NEW.created_at, NEW.target_kind,
           NEW.provider_authorization_attempt_id, NEW.materializer_attempt_ref,
           NEW.materializer_attempt_uid, NEW.materializer_attempt_resource_version, NEW.predecessor_task_id)
       OR (OLD.state = 'COMPLETED' AND OLD.completion_descriptor IS DISTINCT FROM NEW.completion_descriptor) THEN
        RAISE EXCEPTION 'provider credential cleanup snapshot is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.provider_cleanup_task_eligible(p_task_id uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path = '' AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.provider_credential_cleanup_tasks task
        JOIN control_plane.provider_accounts account
          ON account.id = task.provider_account_id AND account.organization_id = task.organization_id
        WHERE task.id = p_task_id
          AND (
              (task.target_kind <> 'CREDENTIAL'
                  AND ((account.state = 'DELETING'
                       AND NOT EXISTS (SELECT 1 FROM control_plane.provider_account_blockers(task.organization_id, task.provider_account_id)))
                    OR EXISTS (SELECT 1 FROM control_plane.provider_authorization_attempts attempt
                               WHERE attempt.id = task.provider_authorization_attempt_id
                                 AND attempt.preparation_state = 'ABANDONED')))
              OR (task.target_kind = 'CREDENTIAL'
                  AND (account.state = 'REVOKED'
                      OR task.provider_credential_revision_id IS DISTINCT FROM account.current_credential_revision_id)
                  AND NOT EXISTS (
                      SELECT 1 FROM control_plane.runtime_leases lease
                      JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
                        AND revision.organization_id = lease.organization_id
                      WHERE revision.provider_credential_revision_id = task.provider_credential_revision_id
                        AND revision.organization_id = task.organization_id
                        AND lease.state = 'CLAIMED')
                  AND NOT EXISTS (
                      SELECT 1 FROM control_plane.assistant_runtime runtime
                      JOIN control_plane.sessions session ON session.ref = runtime.system_session_ref
                        AND session.organization_id = runtime.organization_id
                      WHERE runtime.organization_id = task.organization_id
                        AND session.provider_account_id = task.provider_account_id
                        AND runtime.warm_instance_ref IS NOT NULL))
          )
    );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.provider_queued_run_cancellable(tenant uuid, selected_run uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, control_plane AS $$
 SELECT EXISTS (
  SELECT 1 FROM control_plane.runs run
  WHERE run.organization_id=tenant AND run.id=selected_run AND run.id=run.root_run_id
   AND run.state IN ('QUEUED','RUNNING')
   AND NOT EXISTS (SELECT 1 FROM control_plane.runs sibling WHERE sibling.root_run_id=run.root_run_id
                   AND sibling.id<>run.id AND sibling.state IN ('RUNNING','WAITING_HUMAN','CANCELLING'))
   AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
                   JOIN control_plane.runs sibling ON sibling.id=lease.run_id
                   WHERE sibling.root_run_id=run.root_run_id AND lease.state='CLAIMED')
   AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes node WHERE node.root_run_id=run.root_run_id
                   AND node.type<>'ROOT_PROCESS' AND node.state IN ('RUNNING','WAITING'))
   AND NOT EXISTS (SELECT 1 FROM control_plane.integration_invocations invocation
                   JOIN control_plane.runs sibling ON sibling.id=invocation.run_id
                   WHERE sibling.root_run_id=run.root_run_id
                     AND invocation.state IN ('WAITING_APPROVAL','READY','RUNNING','UNKNOWN_OUTCOME'))
 );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.provider_queued_run_cancellable(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.provider_queued_run_cancellable(uuid,uuid) TO control_plane_runtime;

ALTER TABLE control_plane.provider_credential_cleanup_tasks
 ADD COLUMN recovery_task_ref text,
 ADD COLUMN recovery_generation bigint,
 ADD COLUMN recovery_legacy_last_generation bigint NOT NULL DEFAULT 0;
-- До этой миграции существовали только credential tasks с поколениями от 1.
UPDATE control_plane.provider_credential_cleanup_tasks
 SET recovery_task_ref=ref,recovery_generation=1,recovery_legacy_last_generation=lease_generation;
ALTER TABLE control_plane.provider_credential_cleanup_tasks
 ALTER COLUMN recovery_task_ref SET NOT NULL,
 ALTER COLUMN recovery_generation SET NOT NULL,
 ADD CONSTRAINT provider_cleanup_recovery_pins CHECK (
  recovery_task_ref ~ '^pcct_[A-Za-z0-9_-]{8,88}$' AND recovery_generation>0
  AND recovery_legacy_last_generation BETWEEN 0 AND 32
  AND (recovery_legacy_last_generation=0 OR recovery_generation=1));
-- +goose StatementBegin
CREATE FUNCTION control_plane.assign_provider_cleanup_origin() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.recovery_task_ref IS NULL AND NEW.recovery_generation IS NULL THEN
   NEW.recovery_task_ref:=NEW.ref;
   NEW.recovery_generation:=NEW.lease_generation+1;
  END IF;
 ELSIF ROW(OLD.recovery_task_ref,OLD.recovery_generation,OLD.recovery_legacy_last_generation)
       IS DISTINCT FROM ROW(NEW.recovery_task_ref,NEW.recovery_generation,NEW.recovery_legacy_last_generation) THEN
  RAISE EXCEPTION 'provider cleanup recovery identity is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER provider_cleanup_origin BEFORE INSERT OR UPDATE ON control_plane.provider_credential_cleanup_tasks
 FOR EACH ROW EXECUTE FUNCTION control_plane.assign_provider_cleanup_origin();

-- +goose StatementBegin
DO $$ DECLARE unique_name text; BEGIN
 SELECT constraint_row.conname INTO STRICT unique_name
 FROM pg_constraint constraint_row JOIN pg_attribute attribute
 ON attribute.attrelid=constraint_row.conrelid AND attribute.attname='provider_credential_revision_id'
 WHERE constraint_row.conrelid='control_plane.provider_credential_cleanup_tasks'::regclass
 AND constraint_row.contype='u' AND constraint_row.conkey=ARRAY[attribute.attnum];
 EXECUTE format('ALTER TABLE control_plane.provider_credential_cleanup_tasks DROP CONSTRAINT %I',unique_name);
END $$;
-- +goose StatementEnd
CREATE UNIQUE INDEX provider_cleanup_initial_credential
 ON control_plane.provider_credential_cleanup_tasks(provider_credential_revision_id)
 WHERE predecessor_task_id IS NULL;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'provider account deletion migration is forward-only';
END $$;
-- +goose StatementEnd
