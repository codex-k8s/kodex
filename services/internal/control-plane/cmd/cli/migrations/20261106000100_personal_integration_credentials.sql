-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_connections
    ADD COLUMN credential_policy text NOT NULL DEFAULT 'PROJECT_SHARED'
        CHECK (credential_policy IN ('PROJECT_SHARED', 'INITIATOR_REQUIRED')),
    ADD CONSTRAINT integration_connections_personal_owner_key UNIQUE (organization_id, id);

-- Личный descriptor не входит в shared revisions и не может быть назначен
-- обычному connection.credential_revision_id через существующий foreign key.
CREATE TABLE control_plane.integration_personal_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    state text NOT NULL DEFAULT 'NOT_CONFIGURED'
        CHECK (state IN ('NOT_CONFIGURED', 'CHECK_REQUIRED', 'CHECKING', 'READY', 'INVALID', 'REVOKED')),
    credential_revision_id uuid,
    verified_credential_revision_id uuid,
    verified_definition_digest text CHECK (verified_definition_digest ~ '^[a-f0-9]{64}$'),
    verified_configuration_digest text CHECK (verified_configuration_digest ~ '^[a-f0-9]{64}$'),
    verified_capabilities text[] NOT NULL DEFAULT '{}' CHECK (cardinality(verified_capabilities) <= 256),
    denied_capabilities text[] NOT NULL DEFAULT '{}' CHECK (cardinality(denied_capabilities) <= 256),
    observed_permission_profiles text[] NOT NULL DEFAULT '{}' CHECK (cardinality(observed_permission_profiles) <= 1 AND
        observed_permission_profiles <@ ARRAY['GITLAB_PROJECT_WRITE_SCOPE_V1']::text[]),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id, connection_id)
        REFERENCES control_plane.integration_connections(organization_id, id),
    FOREIGN KEY (organization_id, subject_id)
        REFERENCES control_plane.subjects(organization_id, id),
    UNIQUE (organization_id, connection_id, subject_id),
    UNIQUE (organization_id, id),
    CHECK ((state = 'NOT_CONFIGURED' AND credential_revision_id IS NULL) OR
           (state IN ('CHECK_REQUIRED', 'CHECKING', 'READY', 'INVALID') AND credential_revision_id IS NOT NULL) OR
           state = 'REVOKED'),
    CHECK ((state = 'READY' AND verified_credential_revision_id = credential_revision_id AND
            verified_credential_revision_id IS NOT NULL AND verified_definition_digest IS NOT NULL AND
            verified_configuration_digest IS NOT NULL) OR
           (state <> 'READY' AND verified_credential_revision_id IS NULL AND
            verified_definition_digest IS NULL AND verified_configuration_digest IS NULL AND
            verified_capabilities = '{}' AND denied_capabilities = '{}' AND observed_permission_profiles = '{}' AND expires_at IS NULL))
);

CREATE TABLE control_plane.integration_personal_credential_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL,
    binding_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    secret_ref text NOT NULL CHECK (
        secret_ref ~ '^kodex-system/kodex-integration-credentials#[A-Za-z0-9]([-._A-Za-z0-9]*[A-Za-z0-9])?$'
        AND octet_length(split_part(secret_ref, '#', 2)) <= 253),
    secret_uid uuid NOT NULL,
    secret_resource_version text NOT NULL CHECK (char_length(secret_resource_version) BETWEEN 1 AND 128),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id, binding_id)
        REFERENCES control_plane.integration_personal_credentials(organization_id, id),
    UNIQUE (organization_id, binding_id, id),
    UNIQUE (binding_id, revision),
    UNIQUE (binding_id, secret_uid, secret_resource_version, content_sha256)
);

ALTER TABLE control_plane.integration_personal_credentials
    ADD CONSTRAINT integration_personal_current_revision_owner
        FOREIGN KEY (organization_id, id, credential_revision_id)
        REFERENCES control_plane.integration_personal_credential_revisions(organization_id, binding_id, id),
    ADD CONSTRAINT integration_personal_verified_revision_owner
        FOREIGN KEY (organization_id, id, verified_credential_revision_id)
        REFERENCES control_plane.integration_personal_credential_revisions(organization_id, binding_id, id);

CREATE TRIGGER integration_personal_credential_revisions_immutable
    BEFORE UPDATE OR DELETE ON control_plane.integration_personal_credential_revisions
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_personal_integration_credential() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
DECLARE
    credential_changed boolean;
    newly_revoked boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'personal integration credential history cannot be deleted' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.state <> 'NOT_CONFIGURED' OR NEW.version <> 1 OR NEW.generation <> 1 OR
           NEW.credential_revision_id IS NOT NULL THEN
            RAISE EXCEPTION 'personal integration credential initial state is invalid' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (NEW.id, NEW.ref, NEW.organization_id, NEW.connection_id, NEW.subject_id, NEW.created_at)
       IS DISTINCT FROM
       (OLD.id, OLD.ref, OLD.organization_id, OLD.connection_id, OLD.subject_id, OLD.created_at) OR
       OLD.version = 9223372036854775807 OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'personal integration credential owner or version is invalid' USING ERRCODE = '23514';
    END IF;
    credential_changed := NEW.credential_revision_id IS DISTINCT FROM OLD.credential_revision_id;
    newly_revoked := NEW.state = 'REVOKED' AND OLD.state <> 'REVOKED';
    IF credential_changed OR newly_revoked THEN
        IF OLD.generation = 9223372036854775807 OR NEW.generation <> OLD.generation + 1 OR
           NEW.state NOT IN ('CHECK_REQUIRED', 'REVOKED') OR
           (credential_changed AND (NEW.state <> 'CHECK_REQUIRED' OR NEW.credential_revision_id IS NULL)) THEN
            RAISE EXCEPTION 'personal integration credential generation is invalid' USING ERRCODE = '23514';
        END IF;
        IF credential_changed AND OLD.credential_revision_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM control_plane.integration_personal_credential_revisions previous_revision
            JOIN control_plane.integration_personal_credential_revisions next_revision
              ON next_revision.binding_id = previous_revision.binding_id
             AND next_revision.organization_id = previous_revision.organization_id
             AND next_revision.revision > previous_revision.revision
            WHERE previous_revision.id = OLD.credential_revision_id
              AND next_revision.id = NEW.credential_revision_id
              AND previous_revision.binding_id = OLD.id
              AND previous_revision.organization_id = OLD.organization_id
        ) THEN
            RAISE EXCEPTION 'personal integration credential revision must advance' USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.generation <> OLD.generation OR NOT (
        (OLD.state IN ('CHECK_REQUIRED', 'READY', 'INVALID') AND NEW.state = 'CHECKING') OR
        (OLD.state IN ('READY', 'INVALID') AND NEW.state = 'CHECK_REQUIRED') OR
        (OLD.state = 'CHECKING' AND NEW.state IN ('READY', 'INVALID', 'CHECK_REQUIRED'))
    ) THEN
        RAISE EXCEPTION 'personal integration credential transition is invalid' USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER personal_integration_credential_lifecycle
    BEFORE INSERT OR UPDATE OR DELETE ON control_plane.integration_personal_credentials
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_personal_integration_credential();

ALTER TABLE control_plane.integration_personal_credentials
    ADD CONSTRAINT integration_personal_test_owner_key UNIQUE (organization_id, id, connection_id, subject_id);

CREATE TABLE control_plane.integration_personal_credential_tests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^pct_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    binding_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    credential_revision_id uuid NOT NULL,
    input_schema_version integer NOT NULL DEFAULT 1 CHECK (input_schema_version=1),
    input_snapshot jsonb NOT NULL CHECK (jsonb_typeof(input_snapshot)='object' AND octet_length(input_snapshot::text)<=8192),
    state text NOT NULL DEFAULT 'DUE' CHECK (state IN ('DUE','CLAIMED','PROBING','SUCCEEDED','FAILED','CANCELLED')),
    generation bigint NOT NULL DEFAULT 0 CHECK (generation BETWEEN 0 AND 1),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    lease_ref text,
    fence_digest text CHECK (fence_digest ~ '^[a-f0-9]{64}$'),
    workload_instance text CHECK (char_length(workload_instance) BETWEEN 1 AND 256),
    lease_expires_at timestamptz,
    probe_admitted_at timestamptz,
    outcome text NOT NULL DEFAULT '' CHECK (outcome IN ('','AUTHENTICATED','AUTH_REJECTED','UNAVAILABLE','CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    finished_at timestamptz,
    FOREIGN KEY (organization_id,binding_id,connection_id,subject_id)
        REFERENCES control_plane.integration_personal_credentials(organization_id,id,connection_id,subject_id),
    FOREIGN KEY (organization_id,binding_id,credential_revision_id)
        REFERENCES control_plane.integration_personal_credential_revisions(organization_id,binding_id,id),
    CHECK ((state IN ('CLAIMED','PROBING') AND lease_ref IS NOT NULL AND fence_digest IS NOT NULL AND
            workload_instance IS NOT NULL AND lease_expires_at IS NOT NULL AND generation=1) OR
           (state NOT IN ('CLAIMED','PROBING') AND lease_ref IS NULL AND fence_digest IS NULL AND
            workload_instance IS NULL AND lease_expires_at IS NULL)),
    CHECK ((state IN ('DUE','CLAIMED') AND probe_admitted_at IS NULL) OR
           (state IN ('PROBING','SUCCEEDED') AND probe_admitted_at IS NOT NULL) OR state IN ('FAILED','CANCELLED')),
    CHECK ((state IN ('DUE','CLAIMED','PROBING') AND outcome='' AND finished_at IS NULL) OR
           (state='SUCCEEDED' AND outcome='AUTHENTICATED' AND finished_at IS NOT NULL) OR
           (state='FAILED' AND outcome IN ('AUTH_REJECTED','UNAVAILABLE') AND finished_at IS NOT NULL) OR
           (state='CANCELLED' AND outcome='CANCELLED' AND finished_at IS NOT NULL))
);
CREATE UNIQUE INDEX integration_personal_credential_tests_active
    ON control_plane.integration_personal_credential_tests(binding_id) WHERE state IN ('DUE','CLAIMED','PROBING');
CREATE INDEX integration_personal_credential_tests_due
    ON control_plane.integration_personal_credential_tests(organization_id,created_at,ref)
    WHERE state IN ('DUE','CLAIMED','PROBING');

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_personal_credential_test() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'personal credential test history cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.state<>'DUE' OR NEW.generation<>0 OR NEW.version<>1 THEN
            RAISE EXCEPTION 'personal credential test initial state is invalid' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF (NEW.id,NEW.ref,NEW.organization_id,NEW.connection_id,NEW.binding_id,NEW.subject_id,NEW.credential_revision_id,NEW.input_schema_version,NEW.input_snapshot,NEW.created_at)
       IS DISTINCT FROM
       (OLD.id,OLD.ref,OLD.organization_id,OLD.connection_id,OLD.binding_id,OLD.subject_id,OLD.credential_revision_id,OLD.input_schema_version,OLD.input_snapshot,OLD.created_at) OR
       OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NOT (
        (OLD.state='DUE' AND NEW.state='CLAIMED' AND NEW.generation=1) OR
        (OLD.state='DUE' AND NEW.state='CANCELLED' AND NEW.generation=0) OR
        (OLD.state='CLAIMED' AND NEW.state IN ('PROBING','FAILED','CANCELLED') AND NEW.generation=OLD.generation) OR
        (OLD.state='PROBING' AND NEW.state IN ('SUCCEEDED','FAILED','CANCELLED') AND NEW.generation=OLD.generation)
       ) OR (OLD.probe_admitted_at IS NOT NULL AND NEW.probe_admitted_at IS DISTINCT FROM OLD.probe_admitted_at) OR
       (OLD.probe_admitted_at IS NULL AND NEW.probe_admitted_at IS NOT NULL AND NOT (OLD.state='CLAIMED' AND NEW.state='PROBING')) OR
       (OLD.state='CLAIMED' AND NEW.state='PROBING' AND
        (NEW.lease_ref,NEW.fence_digest,NEW.workload_instance,NEW.lease_expires_at) IS DISTINCT FROM
        (OLD.lease_ref,OLD.fence_digest,OLD.workload_instance,OLD.lease_expires_at)) THEN
        RAISE EXCEPTION 'personal credential test transition is invalid' USING ERRCODE='23514';
    END IF;
    NEW.updated_at:=clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER personal_credential_test_lifecycle BEFORE INSERT OR UPDATE OR DELETE
    ON control_plane.integration_personal_credential_tests FOR EACH ROW EXECUTE FUNCTION control_plane.guard_personal_credential_test();

-- Смена Connection атомарно закрывает незавершённые проверки и их leases.
-- Текущие READY доказательства дополнительно сверяются по точным definition/configuration pins.
-- +goose StatementBegin
CREATE FUNCTION control_plane.cancel_personal_tests_on_connection_change() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF NEW.version IS DISTINCT FROM OLD.version THEN
        UPDATE control_plane.integration_personal_credential_tests
        SET state='CANCELLED',outcome='CANCELLED',version=version+1,finished_at=clock_timestamp(),
            lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL
        WHERE connection_id=NEW.id AND state IN ('DUE','CLAIMED','PROBING');
        UPDATE control_plane.integration_personal_credentials
        SET state='CHECK_REQUIRED',version=version+1
        WHERE connection_id=NEW.id AND state='CHECKING';
        IF (NEW.public_configuration,NEW.definition_version,NEW.definition_digest,NEW.credential_policy)
           IS DISTINCT FROM (OLD.public_configuration,OLD.definition_version,OLD.definition_digest,OLD.credential_policy) THEN
            UPDATE control_plane.integration_personal_credentials
            SET state='CHECK_REQUIRED',version=version+1,verified_credential_revision_id=NULL,
                verified_definition_digest=NULL,verified_configuration_digest=NULL,
                verified_capabilities='{}',denied_capabilities='{}',observed_permission_profiles='{}',expires_at=NULL
            WHERE connection_id=NEW.id AND state IN ('READY','INVALID');
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER personal_tests_connection_change AFTER UPDATE ON control_plane.integration_connections
    FOR EACH ROW EXECUTE FUNCTION control_plane.cancel_personal_tests_on_connection_change();

-- Общая готовность конфигурации для выдачи grant не превращает личный ключ
-- одного пользователя в общую readiness. USE пока сохраняет прежний закрытый
-- допуск; персональная проверка инициатора выполняется отдельным owner path.
-- +goose StatementBegin
CREATE FUNCTION control_plane.integration_connection_configuration_ready(p_connection_id uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
SELECT COALESCE((
    SELECT connection.enabled AND connection.lifecycle_state='ACTIVE'
      AND definition.enabled AND definition.adapter_readiness='READY'
      AND (definition.adapter_owner,definition.execution_route) IN
          (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
      AND CASE connection.credential_policy
        WHEN 'PROJECT_SHARED' THEN connection.state='CONNECTED' AND (
          definition.credential_secret_key IS NULL OR (
            connection.masked_credentials_state='CONFIGURED' AND EXISTS (
              SELECT 1 FROM control_plane.integration_credential_revisions credential
              WHERE credential.id=connection.credential_revision_id
                AND credential.connection_id=connection.id
                AND credential.organization_id=connection.organization_id
            )
          )
        )
        WHEN 'INITIATOR_REQUIRED' THEN connection.state IN ('NOT_CONNECTED','CONNECTED')
          AND definition.adapter_owner='integration-gateway'
          AND definition.execution_route='MANAGED_MCP'
        ELSE false
      END
    FROM control_plane.integration_connections connection
    JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
    WHERE connection.id=p_connection_id
),false);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.integration_connection_configuration_ready(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.integration_connection_configuration_ready(uuid) TO control_plane_runtime;

-- Сохраняем полную исходную функцию, её сигнатуру/ACL и исторические package
-- branches. Ровно одна GRANT-проверка начинает использовать configuration readiness.
-- +goose StatementBegin
DO $personal_grant$
DECLARE
    v_signature regprocedure := 'control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure;
    v_definition text;
    v_before text := E'WHEN NOT connection.enabled OR connection.state<>''CONNECTED''\n              OR NOT connection.credential_ready';
    v_after text := E'WHEN ((p_purpose=''GRANT'' OR connection.credential_policy=''INITIATOR_REQUIRED'') AND NOT control_plane.integration_connection_configuration_ready(connection.id))\n              OR (p_purpose=''USE'' AND connection.credential_policy=''PROJECT_SHARED'' AND (NOT connection.enabled OR connection.state<>''CONNECTED'' OR NOT connection.credential_ready))';
BEGIN
    v_definition := pg_get_functiondef(v_signature);
    IF (length(v_definition)-length(replace(v_definition,v_before,'')))/length(v_before) <> 1 THEN
        RAISE EXCEPTION 'personal grant readiness predecessor changed';
    END IF;
    EXECUTE replace(v_definition,v_before,v_after);
END $personal_grant$;
-- +goose StatementEnd

-- Credential lineage принадлежит намерению invocation, а не его очередной аренде.
-- Ротация не переписывает исходный ключ и не создаёт повтор неизвестного WRITE.
ALTER TABLE control_plane.runs
    ADD CONSTRAINT personal_invocation_root_owner_key UNIQUE (organization_id,id,initiated_by);
ALTER TABLE control_plane.integration_invocations
    ADD CONSTRAINT personal_invocation_connection_key UNIQUE (organization_id,id,connection_id),
    ADD COLUMN claimed_recovery_read_only boolean NOT NULL DEFAULT false;

CREATE TABLE control_plane.integration_personal_invocation_credentials (
    invocation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    root_run_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    binding_id uuid NOT NULL,
    binding_generation bigint NOT NULL CHECK (binding_generation>0),
    credential_revision_id uuid NOT NULL,
    definition_digest text NOT NULL CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
    configuration_digest text NOT NULL CHECK (configuration_digest ~ '^[a-f0-9]{64}$'),
    resource_permission_profile text NOT NULL DEFAULT '' CHECK
      (resource_permission_profile IN ('','CONFLUENCE_SERVER_PAGE_UPDATE_V1',
        'JIRA_SERVER_ISSUE_CREATE_V1','JIRA_SERVER_COMMENT_WRITE_V1','JIRA_SERVER_TRANSITION_APPLY_V1',
        'GITLAB_PROJECT_OBJECT_READ_V1','JIRA_SERVER_PROJECT_OBJECT_READ_V1','CONFLUENCE_SERVER_SPACE_OBJECT_READ_V1',
        'GITLAB_PROJECT_ISSUE_CREATE_V1','GITLAB_PROJECT_BRANCH_CREATE_V1','GITLAB_PROJECT_MERGE_REQUEST_CREATE_V1')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,invocation_id),
    CONSTRAINT personal_invocation_credential_invocation FOREIGN KEY (organization_id,invocation_id,connection_id)
        REFERENCES control_plane.integration_invocations(organization_id,id,connection_id) DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT personal_invocation_credential_root FOREIGN KEY (organization_id,root_run_id,subject_id)
        REFERENCES control_plane.runs(organization_id,id,initiated_by) DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT personal_invocation_credential_binding FOREIGN KEY (organization_id,binding_id,connection_id,subject_id)
        REFERENCES control_plane.integration_personal_credentials(organization_id,id,connection_id,subject_id) DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT personal_invocation_credential_revision FOREIGN KEY (organization_id,binding_id,credential_revision_id)
        REFERENCES control_plane.integration_personal_credential_revisions(organization_id,binding_id,id) DEFERRABLE INITIALLY IMMEDIATE
);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_personal_invocation_credential() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM control_plane.integration_invocations invocation
        JOIN control_plane.runs execution ON execution.id=invocation.run_id
          AND execution.organization_id=invocation.organization_id
        JOIN control_plane.runs root ON root.id=execution.root_run_id
          AND root.organization_id=invocation.organization_id AND root.root_run_id=root.id
        JOIN control_plane.subjects subject ON subject.id=root.initiated_by
          AND subject.organization_id=root.organization_id AND subject.active AND subject.kind='USER'
        JOIN control_plane.integration_personal_credentials binding ON binding.id=NEW.binding_id
          AND binding.organization_id=NEW.organization_id AND binding.connection_id=NEW.connection_id
          AND binding.subject_id=NEW.subject_id AND binding.generation=NEW.binding_generation
          AND binding.state='READY' AND binding.credential_revision_id=NEW.credential_revision_id
          AND binding.verified_credential_revision_id=NEW.credential_revision_id
          AND binding.verified_definition_digest=NEW.definition_digest
          AND binding.verified_configuration_digest=NEW.configuration_digest
          AND (binding.expires_at IS NULL OR binding.expires_at>clock_timestamp())
        JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
          AND connection.organization_id=invocation.organization_id
          AND connection.credential_policy='INITIATOR_REQUIRED'
          AND control_plane.integration_connection_configuration_ready(connection.id)
        WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
          AND root.id=NEW.root_run_id AND root.initiated_by=NEW.subject_id
          AND root.source NOT IN ('SCHEDULE','INTEGRATION')
          AND root.state IN ('RUNNING','WAITING_HUMAN')
          AND invocation.definition_digest=NEW.definition_digest
          AND (
            (NEW.resource_permission_profile=''
              AND invocation.capability_key=ANY(binding.verified_capabilities)
              AND NOT (connection.definition_key='confluence-server-dc'
                AND invocation.capability_key='confluence.page.update')
              AND NOT (connection.definition_key='jira-server-dc'
                AND invocation.capability_key IN ('jira.issue.create','jira.issue.comment.write','jira.issue.transition.apply',
                  'jira.issue.search','jira.issue.read','jira.issue.transition.list'))
              AND NOT (connection.definition_key='confluence-server-dc'
                AND invocation.capability_key IN ('confluence.page.search','confluence.page.read'))
              AND NOT (connection.definition_key='gitlab'
                AND invocation.capability_key IN ('gitlab.repository.file.read','gitlab.issue.read','gitlab.merge_request.read','gitlab.pipeline.read',
                  'gitlab.issue.create','gitlab.branch.create','gitlab.merge_request.create')))
            OR (NEW.resource_permission_profile='CONFLUENCE_SERVER_PAGE_UPDATE_V1'
              AND connection.definition_key='confluence-server-dc'
              AND invocation.capability_key='confluence.page.update'
              AND invocation.operation='confluence.page.update'
              AND invocation.risk='WRITE' AND invocation.approval_policy='HUMAN_EACH_EFFECT'
              AND invocation.resource_kind='CONFLUENCE_SPACE'
              AND binding.verified_capabilities @> ARRAY['confluence.space.read','confluence.identity.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['confluence.space.read','confluence.identity.read']::text[])
            OR (connection.definition_key='jira-server-dc'
              AND ((NEW.resource_permission_profile='JIRA_SERVER_ISSUE_CREATE_V1' AND invocation.capability_key='jira.issue.create')
                OR (NEW.resource_permission_profile='JIRA_SERVER_COMMENT_WRITE_V1' AND invocation.capability_key='jira.issue.comment.write')
                OR (NEW.resource_permission_profile='JIRA_SERVER_TRANSITION_APPLY_V1' AND invocation.capability_key='jira.issue.transition.apply'))
              AND invocation.operation=invocation.capability_key
              AND invocation.risk='WRITE' AND invocation.approval_policy='HUMAN_EACH_EFFECT'
              AND invocation.resource_kind='JIRA_PROJECT'
              AND binding.verified_capabilities @> ARRAY['jira.project.read','jira.identity.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['jira.project.read','jira.identity.read']::text[])
            OR (connection.definition_key='gitlab'
              AND ((NEW.resource_permission_profile='GITLAB_PROJECT_ISSUE_CREATE_V1'
                  AND invocation.capability_key='gitlab.issue.create' AND invocation.risk='WRITE')
                OR (NEW.resource_permission_profile='GITLAB_PROJECT_BRANCH_CREATE_V1'
                  AND invocation.capability_key='gitlab.branch.create' AND invocation.risk='SENSITIVE')
                OR (NEW.resource_permission_profile='GITLAB_PROJECT_MERGE_REQUEST_CREATE_V1'
                  AND invocation.capability_key='gitlab.merge_request.create' AND invocation.risk='SENSITIVE'))
              AND invocation.operation=invocation.capability_key AND invocation.approval_policy='HUMAN_EACH_EFFECT'
              AND invocation.resource_kind='GITLAB_PROJECT'
              AND binding.observed_permission_profiles @> ARRAY['GITLAB_PROJECT_WRITE_SCOPE_V1']::text[]
              AND binding.verified_capabilities @> ARRAY['gitlab.project.metadata.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['gitlab.project.metadata.read']::text[])
            OR (NEW.resource_permission_profile='GITLAB_PROJECT_OBJECT_READ_V1'
              AND connection.definition_key='gitlab'
              AND invocation.capability_key IN ('gitlab.repository.file.read','gitlab.issue.read','gitlab.merge_request.read','gitlab.pipeline.read')
              AND invocation.operation=invocation.capability_key
              AND invocation.risk='READ' AND invocation.approval_policy='NONE'
              AND invocation.resource_kind='GITLAB_PROJECT'
              AND binding.verified_capabilities @> ARRAY['gitlab.project.metadata.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['gitlab.project.metadata.read']::text[])
            OR (NEW.resource_permission_profile='JIRA_SERVER_PROJECT_OBJECT_READ_V1'
              AND connection.definition_key='jira-server-dc'
              AND invocation.capability_key IN ('jira.issue.search','jira.issue.read','jira.issue.transition.list')
              AND invocation.operation=invocation.capability_key
              AND invocation.risk='READ' AND invocation.approval_policy='NONE'
              AND invocation.resource_kind='JIRA_PROJECT'
              AND binding.verified_capabilities @> ARRAY['jira.project.read','jira.identity.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['jira.project.read','jira.identity.read']::text[])
            OR (NEW.resource_permission_profile='CONFLUENCE_SERVER_SPACE_OBJECT_READ_V1'
              AND connection.definition_key='confluence-server-dc'
              AND invocation.capability_key IN ('confluence.page.search','confluence.page.read')
              AND invocation.operation=invocation.capability_key
              AND invocation.risk='READ' AND invocation.approval_policy='NONE'
              AND invocation.resource_kind='CONFLUENCE_SPACE'
              AND binding.verified_capabilities @> ARRAY['confluence.space.read','confluence.identity.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['confluence.space.read','confluence.identity.read']::text[])
          )
          AND NOT invocation.capability_key=ANY(binding.denied_capabilities)
    ) THEN
        RAISE EXCEPTION 'personal invocation credential lineage is invalid' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER personal_invocation_credential_owner
    BEFORE INSERT ON control_plane.integration_personal_invocation_credentials
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_personal_invocation_credential();
CREATE TRIGGER personal_invocation_credential_immutable
    BEFORE UPDATE OR DELETE ON control_plane.integration_personal_invocation_credentials
    FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
    EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.integration_personal_invocation_credentials FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.integration_personal_invocation_credentials TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_personal_invocation_credential() FROM PUBLIC;

CREATE TABLE control_plane.integration_personal_invocation_admissions (
    organization_id uuid NOT NULL,
    invocation_id uuid NOT NULL,
    generation bigint NOT NULL CHECK (generation>0),
    lease_ref text NOT NULL CHECK (char_length(lease_ref) BETWEEN 8 AND 96),
    fence_digest text NOT NULL CHECK (fence_digest ~ '^[a-f0-9]{64}$'),
    workload_instance text NOT NULL CHECK (char_length(workload_instance) BETWEEN 1 AND 256),
    recovery_read_only boolean NOT NULL,
    admitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (invocation_id,generation),
    UNIQUE (lease_ref),
    CONSTRAINT personal_invocation_admission_pin FOREIGN KEY (organization_id,invocation_id)
        REFERENCES control_plane.integration_personal_invocation_credentials(organization_id,invocation_id)
        DEFERRABLE INITIALLY IMMEDIATE
);
CREATE UNIQUE INDEX personal_invocation_single_execution
    ON control_plane.integration_personal_invocation_admissions(invocation_id) WHERE NOT recovery_read_only;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_personal_invocation_admission() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM control_plane.integration_invocations invocation
        WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
          AND invocation.state='RUNNING' AND invocation.generation=NEW.generation
          AND invocation.lease_ref=NEW.lease_ref AND invocation.claimed_lease_ref=NEW.lease_ref
          AND invocation.effect_fence_digest=NEW.fence_digest AND invocation.claimed_fence_digest=NEW.fence_digest
          AND invocation.workload_instance=NEW.workload_instance AND invocation.claimed_workload='integration-gateway'
          AND invocation.claimed_recovery_read_only=NEW.recovery_read_only
          AND invocation.lease_expires_at>clock_timestamp()) THEN
        RAISE EXCEPTION 'personal invocation admission lease is invalid' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER personal_invocation_admission_owner
    BEFORE INSERT ON control_plane.integration_personal_invocation_admissions
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_personal_invocation_admission();
CREATE TRIGGER personal_invocation_admission_immutable
    BEFORE UPDATE OR DELETE ON control_plane.integration_personal_invocation_admissions
    FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
    EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.integration_personal_invocation_admissions FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.integration_personal_invocation_admissions TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_personal_invocation_admission() FROM PUBLIC;

-- Меняем лишь известный graph delta. Старый function body, ACL, весь прежний
-- граф и точные четыре новых FK проверяются до обновления закрытых fingerprints.
-- +goose StatementBegin
DO $personal_purge$
DECLARE
    v_function oid:='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure;
    v_definition text; v_source text; v_expected text; v_old_nodes text; v_old_edges text;
    v_nodes oid[]; v_old oid[]; v_added oid[]:=ARRAY['control_plane.integration_personal_invocation_credentials'::regclass,
        'control_plane.integration_personal_invocation_admissions'::regclass]::oid[];
    v_count integer; v_digest text; v_new_nodes text; v_new_edges text; v_links jsonb;
BEGIN
    SELECT pg_get_functiondef(procedure_row.oid),procedure_row.prosrc INTO STRICT v_definition,v_source
    FROM pg_proc procedure_row WHERE procedure_row.oid=v_function
      AND procedure_row.proowner='control_plane_owner'::regrole AND procedure_row.prosecdef
      AND procedure_row.provolatile='v' AND procedure_row.prorettype='integer'::regtype
      AND NOT procedure_row.proretset AND procedure_row.proconfig=ARRAY['search_path=pg_catalog']
      AND has_function_privilege('control_plane_runtime',procedure_row.oid,'EXECUTE')
      AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(procedure_row.proacl,acldefault('f',procedure_row.proowner))) acl
        WHERE acl.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
          OR acl.grantee='control_plane_runtime'::regrole AND (acl.is_grantable OR acl.privilege_type<>'EXECUTE'));
    v_old_nodes:=substring(v_source FROM 'IF v_nodes <> 109 OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN');
    v_old_edges:=substring(v_source FROM 'IF v_edges <> 30[34] OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
    IF v_old_nodes IS NULL OR v_old_edges IS NULL OR
       md5(replace(replace(v_source,v_old_edges,
         'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'),v_old_nodes,
         'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0' THEN
        RAISE EXCEPTION 'personal invocation purge predecessor changed';
    END IF;
    WITH RECURSIVE nodes(id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
      SELECT constraint_row.conrelid FROM pg_constraint constraint_row JOIN nodes parent ON constraint_row.confrelid=parent.id
      WHERE constraint_row.contype='f' AND constraint_row.connamespace='control_plane'::regnamespace)
    SELECT array_agg(id),array_agg(id) FILTER (WHERE NOT id=ANY(v_added)) INTO v_nodes,v_old FROM nodes;
    SELECT count(*),md5(string_agg(namespace_row.nspname||'.'||relation.relname,E'\n' ORDER BY namespace_row.nspname,relation.relname))
      INTO v_count,v_digest FROM pg_class relation JOIN pg_namespace namespace_row ON namespace_row.oid=relation.relnamespace
      WHERE relation.oid=ANY(v_old);
    IF v_count<>109 OR v_old_nodes IS DISTINCT FROM format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',v_count,v_digest) THEN
        RAISE EXCEPTION 'personal invocation purge predecessor nodes changed';
    END IF;
    SELECT count(*),md5(string_agg(child_namespace.nspname||'.'||child.relname||'|'||constraint_row.conname||'|'||
      parent_namespace.nspname||'.'||parent.relname||'|'||constraint_row.confdeltype::text,E'\n'
      ORDER BY child_namespace.nspname,child.relname,constraint_row.conname)) INTO v_count,v_digest
    FROM pg_constraint constraint_row JOIN pg_class child ON child.oid=constraint_row.conrelid
    JOIN pg_namespace child_namespace ON child_namespace.oid=child.relnamespace
    JOIN pg_class parent ON parent.oid=constraint_row.confrelid JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent.relnamespace
    WHERE constraint_row.contype='f' AND constraint_row.conrelid=ANY(v_old) AND constraint_row.confrelid=ANY(v_old);
    IF v_count NOT IN (303,304) OR v_old_edges IS DISTINCT FROM format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest) THEN
        RAISE EXCEPTION 'personal invocation purge predecessor edges changed';
    END IF;
    SELECT jsonb_agg(jsonb_build_array(constraint_row.conrelid::regclass::text,constraint_row.conname,constraint_row.confrelid::regclass::text,
      constraint_row.confdeltype::text,constraint_row.condeferrable,constraint_row.condeferred,
      constraint_row.convalidated,constraint_row.conenforced) ORDER BY constraint_row.conrelid::regclass::text,constraint_row.conname) INTO v_links
    FROM pg_constraint constraint_row WHERE constraint_row.contype='f'
      AND (constraint_row.conrelid=ANY(v_added) OR constraint_row.confrelid=ANY(v_added));
    IF v_links IS DISTINCT FROM '[
      ["control_plane.integration_personal_invocation_admissions","personal_invocation_admission_pin","control_plane.integration_personal_invocation_credentials","a",true,false,true,true],
      ["control_plane.integration_personal_invocation_credentials","personal_invocation_credential_binding","control_plane.integration_personal_credentials","a",true,false,true,true],
      ["control_plane.integration_personal_invocation_credentials","personal_invocation_credential_invocation","control_plane.integration_invocations","a",true,false,true,true],
      ["control_plane.integration_personal_invocation_credentials","personal_invocation_credential_revision","control_plane.integration_personal_credential_revisions","a",true,false,true,true],
      ["control_plane.integration_personal_invocation_credentials","personal_invocation_credential_root","control_plane.runs","a",true,false,true,true]]'::jsonb
      OR cardinality(v_nodes)<>111 OR cardinality(v_old)<>109 THEN
        RAISE EXCEPTION 'personal invocation purge graph delta changed';
    END IF;
    SELECT count(*),md5(string_agg(child_namespace.nspname||'.'||child.relname||'|'||constraint_row.conname||'|'||
      parent_namespace.nspname||'.'||parent.relname||'|'||constraint_row.confdeltype::text,E'\n'
      ORDER BY child_namespace.nspname,child.relname,constraint_row.conname)) INTO v_count,v_digest
    FROM pg_constraint constraint_row JOIN pg_class child ON child.oid=constraint_row.conrelid
    JOIN pg_namespace child_namespace ON child_namespace.oid=child.relnamespace
    JOIN pg_class parent ON parent.oid=constraint_row.confrelid JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent.relnamespace
    WHERE constraint_row.contype='f' AND constraint_row.conrelid=ANY(v_nodes) AND constraint_row.confrelid=ANY(v_nodes);
    IF v_count NOT IN (306,307) THEN RAISE EXCEPTION 'personal invocation purge edge count changed'; END IF;
    v_new_edges:=format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest);
    SELECT md5(string_agg(namespace_row.nspname||'.'||relation.relname,E'\n' ORDER BY namespace_row.nspname,relation.relname))
      INTO v_digest FROM pg_class relation JOIN pg_namespace namespace_row ON namespace_row.oid=relation.relnamespace
      WHERE relation.oid=ANY(v_nodes);
    v_new_nodes:=format('IF v_nodes <> 111 OR v_node_fingerprint <> %L THEN',v_digest);
    v_expected:=replace(replace(v_source,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
    EXECUTE replace(replace(v_definition,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
    IF NOT EXISTS (SELECT 1 FROM pg_proc procedure_row WHERE procedure_row.oid=v_function AND procedure_row.prosrc=v_expected) THEN
        RAISE EXCEPTION 'personal invocation purge function readback failed';
    END IF;
END $personal_purge$;
-- +goose StatementEnd

REVOKE ALL ON control_plane.integration_personal_credentials,
    control_plane.integration_personal_credential_revisions FROM control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.integration_personal_credentials TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.integration_personal_credential_revisions TO control_plane_runtime;
REVOKE DELETE ON control_plane.integration_personal_credentials FROM control_plane_runtime;
REVOKE UPDATE, DELETE ON control_plane.integration_personal_credential_revisions FROM control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_personal_integration_credential() FROM PUBLIC;
REVOKE ALL ON control_plane.integration_personal_credential_tests FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.integration_personal_credential_tests TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_personal_credential_test(),
    control_plane.cancel_personal_tests_on_connection_change() FROM PUBLIC;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'personal integration credentials migration is forward-only'; END $$;
-- +goose StatementEnd
