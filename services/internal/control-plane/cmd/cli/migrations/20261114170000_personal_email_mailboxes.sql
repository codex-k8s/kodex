-- +goose Up
SET ROLE control_plane_owner;

-- Старые credentials не получают владельца по created_by автоматически.
-- Явная первоначальная настройка требует новых descriptor revisions.
ALTER TABLE control_plane.integration_connections
  ADD COLUMN mailbox_owner_subject_id uuid REFERENCES control_plane.subjects(id),
  ADD COLUMN mailbox_owner_configured_by uuid REFERENCES control_plane.subjects(id),
  ADD COLUMN mailbox_owner_generation bigint NOT NULL DEFAULT 0 CHECK (mailbox_owner_generation BETWEEN 0 AND 9007199254740991),
  ADD COLUMN mailbox_credential_version bigint NOT NULL DEFAULT 0
    CHECK (mailbox_credential_version>=0 AND mailbox_credential_version<=version
      AND (definition_key='email' OR mailbox_credential_version=0)),
  ADD COLUMN mailbox_owner_state text NOT NULL DEFAULT 'UNCONFIGURED'
    CHECK (mailbox_owner_state IN ('UNCONFIGURED','ACTIVE','REVOKED')),
  ADD CONSTRAINT email_mailbox_owner_shape CHECK (
    (mailbox_owner_state='UNCONFIGURED' AND mailbox_owner_generation=0
      AND mailbox_owner_subject_id IS NULL AND mailbox_owner_configured_by IS NULL)
    OR (definition_key='email' AND mailbox_owner_state IN ('ACTIVE','REVOKED')
      AND mailbox_owner_generation>0 AND mailbox_owner_subject_id IS NOT NULL
      AND mailbox_owner_configured_by IS NOT NULL)
  );

ALTER TABLE control_plane.email_credential_descriptors
  ADD COLUMN mailbox_owner_generation bigint CHECK (mailbox_owner_generation>0);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_email_mailbox_credential_owner()
RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
DECLARE owner_kind text; owner_active boolean; operator_kind text; operator_active boolean;
BEGIN
  IF TG_TABLE_NAME='integration_connections' THEN
    IF TG_OP='INSERT' THEN
      IF NEW.mailbox_owner_state<>'UNCONFIGURED' OR NEW.mailbox_owner_generation<>0 THEN
        RAISE EXCEPTION 'mailbox owner must be assigned by the owner command' USING ERRCODE='23514';
      END IF;
      RETURN NEW;
    END IF;
    IF NEW.mailbox_credential_version<OLD.mailbox_credential_version THEN
      RAISE EXCEPTION 'mailbox credential version cannot decrease' USING ERRCODE='23514';
    END IF;
    IF (NEW.mailbox_owner_subject_id,NEW.mailbox_owner_configured_by,NEW.mailbox_owner_generation,NEW.mailbox_owner_state)
       IS NOT DISTINCT FROM
       (OLD.mailbox_owner_subject_id,OLD.mailbox_owner_configured_by,OLD.mailbox_owner_generation,OLD.mailbox_owner_state) THEN
      IF NEW.definition_key='email' AND NEW.mailbox_owner_state<>'UNCONFIGURED'
         AND NEW.credential_policy<>OLD.credential_policy THEN
        RAISE EXCEPTION 'mailbox policy requires a fresh owner generation' USING ERRCODE='23514';
      END IF;
      RETURN NEW;
    END IF;
    IF NEW.definition_key<>'email' OR NEW.mailbox_owner_generation<>OLD.mailbox_owner_generation+1
       OR NEW.mailbox_owner_state NOT IN ('ACTIVE','REVOKED') THEN
      RAISE EXCEPTION 'mailbox owner generation is invalid' USING ERRCODE='23514';
    END IF;
    SELECT kind,active INTO owner_kind,owner_active FROM control_plane.subjects
      WHERE id=NEW.mailbox_owner_subject_id AND organization_id=NEW.organization_id;
    SELECT kind,active INTO operator_kind,operator_active FROM control_plane.subjects
      WHERE id=NEW.mailbox_owner_configured_by AND organization_id=NEW.organization_id;
    IF owner_kind IS NULL OR operator_kind IS DISTINCT FROM 'USER' OR NOT operator_active
       OR (NEW.mailbox_owner_state='ACTIVE' AND NOT owner_active)
       OR (NEW.credential_policy='INITIATOR_REQUIRED'
         AND (owner_kind<>'USER' OR NEW.mailbox_owner_subject_id<>NEW.mailbox_owner_configured_by))
       OR (NEW.credential_policy='PROJECT_SHARED' AND owner_kind<>'SERVICE') THEN
      RAISE EXCEPTION 'mailbox owner identity is invalid' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM control_plane.integration_connections connection
    JOIN control_plane.subjects subject ON subject.id=connection.mailbox_owner_subject_id
      AND subject.organization_id=connection.organization_id AND subject.active
    JOIN control_plane.subjects configurer ON configurer.id=connection.mailbox_owner_configured_by
      AND configurer.organization_id=connection.organization_id AND configurer.kind='USER' AND configurer.active
    WHERE connection.id=NEW.connection_id AND connection.organization_id=NEW.organization_id
      AND connection.definition_key='email' AND connection.lifecycle_state='ACTIVE'
      AND connection.mailbox_owner_state='ACTIVE'
      AND connection.mailbox_owner_generation=NEW.mailbox_owner_generation
      AND connection.mailbox_owner_configured_by=NEW.created_by
      AND ((connection.credential_policy='INITIATOR_REQUIRED' AND subject.kind='USER'
        AND subject.id=configurer.id) OR (connection.credential_policy='PROJECT_SHARED' AND subject.kind='SERVICE'))
  ) THEN
    RAISE EXCEPTION 'mailbox credential owner is unavailable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_email_mailbox_credential_owner() FROM PUBLIC;
CREATE TRIGGER email_mailbox_credential_owner_guard
  BEFORE INSERT OR UPDATE ON control_plane.integration_connections
  FOR EACH ROW EXECUTE FUNCTION control_plane.guard_email_mailbox_credential_owner();
CREATE TRIGGER email_credential_owner_guard
  BEFORE INSERT ON control_plane.email_credential_descriptors
  FOR EACH ROW EXECUTE FUNCTION control_plane.guard_email_mailbox_credential_owner();

-- Pin остаётся immutable при обычной работе. Только существующий авторитетный
-- project purge context допускает удаление; отложенные FK нужны его атомарному
-- удалению всего захваченного графа независимо от порядка relation OID.
CREATE TABLE control_plane.email_invocation_mailbox_bindings (
  invocation_id uuid PRIMARY KEY REFERENCES control_plane.integration_invocations(id) DEFERRABLE INITIALLY IMMEDIATE,
  organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
  connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id) DEFERRABLE INITIALLY IMMEDIATE,
  root_run_id uuid NOT NULL REFERENCES control_plane.runs(id) DEFERRABLE INITIALLY IMMEDIATE,
  initiator_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
  owner_subject_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
  owner_generation bigint NOT NULL CHECK (owner_generation>0),
  mailbox_ref text NOT NULL CHECK (mailbox_ref<>''),
  mailbox_revision bigint NOT NULL CHECK (mailbox_revision>0),
  mailbox_source_digest text NOT NULL CHECK (mailbox_source_digest ~ '^[0-9a-f]{64}$'),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER email_invocation_mailbox_binding_immutable
  BEFORE UPDATE OR DELETE ON control_plane.email_invocation_mailbox_bindings
  FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
  EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON TABLE control_plane.email_invocation_mailbox_bindings FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.email_invocation_mailbox_bindings TO control_plane_runtime;

-- Это только exact mailbox context существующего consent, не второй consent ledger.
CREATE TABLE control_plane.email_approval_scope_bindings (
  approval_scope_id uuid PRIMARY KEY REFERENCES control_plane.integration_approval_scopes(id) DEFERRABLE INITIALLY IMMEDIATE,
  organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
  connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id) DEFERRABLE INITIALLY IMMEDIATE,
  owner_subject_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
  owner_generation bigint NOT NULL CHECK (owner_generation>0),
  mailbox_ref text NOT NULL CHECK (mailbox_ref<>''),
  mailbox_revision bigint NOT NULL CHECK (mailbox_revision>0),
  mailbox_source_digest text NOT NULL CHECK (mailbox_source_digest ~ '^[0-9a-f]{64}$'),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER email_approval_scope_binding_immutable
  BEFORE UPDATE OR DELETE ON control_plane.email_approval_scope_bindings
  FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
  EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON TABLE control_plane.email_approval_scope_bindings FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.email_approval_scope_bindings TO control_plane_runtime;

-- Один typed owner predicate применяется к общей canonical claim выборке.
-- Он не выдаёт lease, не принимает actor/policy из caller и сам не разрешает effect.
-- +goose StatementBegin
CREATE FUNCTION control_plane.email_mailbox_binding_current(p_pin control_plane.email_invocation_mailbox_bindings)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS(
  SELECT 1 FROM control_plane.integration_invocations invocation
  JOIN control_plane.integration_connections connection ON connection.id=(p_pin).connection_id
    AND connection.organization_id=(p_pin).organization_id AND connection.definition_key='email'
    AND connection.enabled AND connection.lifecycle_state='ACTIVE' AND connection.state='CONNECTED'
    AND connection.mailbox_owner_state='ACTIVE' AND connection.mailbox_owner_subject_id=(p_pin).owner_subject_id
    AND connection.mailbox_owner_generation=(p_pin).owner_generation
  JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=(p_pin).organization_id
    AND execution.root_run_id=(p_pin).root_run_id
  JOIN control_plane.runs root ON root.id=(p_pin).root_run_id AND root.organization_id=(p_pin).organization_id
    AND root.root_run_id=root.id AND root.initiated_by=(p_pin).initiator_id AND root.state IN ('RUNNING','WAITING_HUMAN')
  JOIN control_plane.subjects actor ON actor.id=(p_pin).initiator_id AND actor.organization_id=(p_pin).organization_id AND actor.active
  JOIN control_plane.subjects owner ON owner.id=(p_pin).owner_subject_id AND owner.organization_id=(p_pin).organization_id AND owner.active
  JOIN control_plane.subjects configurer ON configurer.id=connection.mailbox_owner_configured_by
    AND configurer.organization_id=(p_pin).organization_id AND configurer.kind='USER' AND configurer.active
  JOIN control_plane.email_mailbox_projections mailbox ON mailbox.organization_id=(p_pin).organization_id
    AND mailbox.connection_id=(p_pin).connection_id AND mailbox.ref=(p_pin).mailbox_ref AND mailbox.enabled AND NOT mailbox.removed
    AND mailbox.revision=(p_pin).mailbox_revision AND mailbox.safe_projection->>'source_digest'=(p_pin).mailbox_source_digest
    AND mailbox.safe_projection->>'credential_owner_generation'=(p_pin).owner_generation::text
    AND (mailbox.safe_projection->>'credential_generation')::bigint>=connection.mailbox_credential_version
  JOIN control_plane.email_configuration_watermark watermark ON watermark.singleton AND watermark.revision=mailbox.document_revision
  WHERE invocation.id=(p_pin).invocation_id
    AND invocation.organization_id=(p_pin).organization_id AND invocation.connection_id=(p_pin).connection_id
    AND ((connection.credential_policy='INITIATOR_REQUIRED' AND actor.kind='USER' AND actor.id=owner.id
      AND owner.kind='USER' AND owner.id=configurer.id AND root.source NOT IN ('SCHEDULE','INTEGRATION'))
      OR (connection.credential_policy='PROJECT_SHARED' AND owner.kind='SERVICE'
        AND (actor.kind='USER' OR actor.kind='SERVICE' AND actor.id=owner.id)))
);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.email_mailbox_binding_current(control_plane.email_invocation_mailbox_bindings) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.email_mailbox_binding_current(control_plane.email_invocation_mailbox_bindings) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.email_invocation_mailbox_current(p_invocation_id uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS(SELECT 1 FROM control_plane.email_invocation_mailbox_bindings pin
  WHERE pin.invocation_id=p_invocation_id AND control_plane.email_mailbox_binding_current(pin));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.email_invocation_mailbox_current(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.email_invocation_mailbox_current(uuid) TO control_plane_runtime;

-- INSERT проверяет тот же current owner predicate до сохранения immutable pin.
-- Scope pin разрешается только для первоначальной invocation своего настоящего Gate.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_email_mailbox_context_pin()
RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
BEGIN
  IF TG_TABLE_NAME='email_invocation_mailbox_bindings' THEN
    IF NOT control_plane.email_mailbox_binding_current(NEW::control_plane.email_invocation_mailbox_bindings)
       OR NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
         WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
           AND invocation.resource_kind='EMAIL_SENDER' AND invocation.state IN ('READY','WAITING_APPROVAL')) THEN
      RAISE EXCEPTION 'email invocation mailbox pin is invalid' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
  END IF;
  IF NOT EXISTS(
    SELECT 1 FROM control_plane.email_invocation_mailbox_bindings pin
    JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id
      AND invocation.organization_id=pin.organization_id AND invocation.connection_id=pin.connection_id
      AND invocation.approval_policy='HUMAN_SCOPED' AND invocation.state='WAITING_APPROVAL'
      AND invocation.approval_scope_id=NEW.approval_scope_id
    JOIN control_plane.integration_approval_scopes approval ON approval.id=NEW.approval_scope_id
      AND approval.organization_id=pin.organization_id AND approval.connection_id=pin.connection_id
      AND approval.grant_id=invocation.grant_id AND approval.grant_version=invocation.grant_version
      AND approval.capability_key=invocation.capability_key AND approval.definition_digest=invocation.definition_digest
      AND approval.root_run_id=pin.root_run_id AND approval.revoked_at IS NULL
      AND approval.expires_at>clock_timestamp()
    JOIN control_plane.owner_gates gate ON gate.id=approval.origin_gate_id
      AND gate.organization_id=pin.organization_id AND gate.integration_invocation_id=invocation.id
      AND gate.root_run_id=pin.root_run_id AND gate.state='OPEN'
    JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=pin.organization_id
      AND node.agent_id=approval.agent_id
    JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=pin.organization_id
      AND execution.project_id IS NOT DISTINCT FROM approval.project_id
    WHERE pin.organization_id=NEW.organization_id AND pin.connection_id=NEW.connection_id
      AND pin.owner_subject_id=NEW.owner_subject_id AND pin.owner_generation=NEW.owner_generation
      AND pin.mailbox_ref=NEW.mailbox_ref AND pin.mailbox_revision=NEW.mailbox_revision
      AND pin.mailbox_source_digest=NEW.mailbox_source_digest
      AND control_plane.email_mailbox_binding_current(pin)
  ) THEN
    RAISE EXCEPTION 'email scoped mailbox pin is invalid' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_email_mailbox_context_pin() FROM PUBLIC;
CREATE TRIGGER email_invocation_mailbox_context_guard
  BEFORE INSERT ON control_plane.email_invocation_mailbox_bindings
  FOR EACH ROW EXECUTE FUNCTION control_plane.guard_email_mailbox_context_pin();
CREATE TRIGGER email_approval_mailbox_context_guard
  BEFORE INSERT ON control_plane.email_approval_scope_bindings
  FOR EACH ROW EXECUTE FUNCTION control_plane.guard_email_mailbox_context_pin();

-- Снимок проверки содержит назначенные владельцем mailbox coordinates. Обычный generic token
-- отсутствует; текущий конфигуратор не получает право проверять credentials другого mailbox owner.
-- +goose StatementBegin
CREATE FUNCTION control_plane.email_health_input_snapshot(p_organization_id uuid,p_connection_id uuid,p_actor_id uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
SELECT jsonb_build_object('mailboxRef',mailbox.ref,'mailboxRevision',mailbox.revision,
  'mailboxSourceDigest',mailbox.safe_projection->>'source_digest',
  'mailboxOwnerGeneration',connection.mailbox_owner_generation)
FROM control_plane.integration_connections connection
JOIN control_plane.subjects owner ON owner.id=connection.mailbox_owner_subject_id
  AND owner.organization_id=connection.organization_id AND owner.active
JOIN control_plane.subjects configurer ON configurer.id=connection.mailbox_owner_configured_by
  AND configurer.organization_id=connection.organization_id AND configurer.kind='USER' AND configurer.active
JOIN control_plane.email_mailbox_projections mailbox ON mailbox.organization_id=connection.organization_id
  AND mailbox.connection_id=connection.id AND mailbox.enabled AND NOT mailbox.removed
  AND mailbox.ref=connection.public_configuration->>'mailbox_id'
  AND mailbox.safe_projection->>'sender'=connection.public_configuration->>'from_address'
  AND mailbox.safe_projection->>'credential_owner_generation'=connection.mailbox_owner_generation::text
  AND (mailbox.safe_projection->>'credential_generation')::bigint>=connection.mailbox_credential_version
JOIN control_plane.email_configuration_watermark watermark ON watermark.singleton AND watermark.revision=mailbox.document_revision
WHERE connection.organization_id=p_organization_id AND connection.id=p_connection_id
  AND connection.definition_key='email' AND connection.enabled AND connection.lifecycle_state='ACTIVE'
  AND connection.mailbox_owner_state='ACTIVE' AND configurer.id=p_actor_id
  AND ((connection.credential_policy='INITIATOR_REQUIRED' AND owner.kind='USER' AND owner.id=configurer.id)
    OR (connection.credential_policy='PROJECT_SHARED' AND owner.kind='SERVICE'));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.email_health_input_snapshot(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.email_health_input_snapshot(uuid,uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.email_connection_test_mailbox_current(p_test_id uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS(SELECT 1 FROM control_plane.integration_connection_tests test
  CROSS JOIN LATERAL (SELECT control_plane.email_health_input_snapshot(test.organization_id,test.connection_id,test.created_by) AS current) expected
  WHERE test.id=p_test_id AND expected.current IS NOT NULL
    AND test.input_snapshot->'mailboxRef'=expected.current->'mailboxRef'
    AND test.input_snapshot->'mailboxRevision'=expected.current->'mailboxRevision'
    AND test.input_snapshot->'mailboxSourceDigest'=expected.current->'mailboxSourceDigest'
    AND test.input_snapshot->'mailboxOwnerGeneration'=expected.current->'mailboxOwnerGeneration');
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.email_connection_test_mailbox_current(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.email_connection_test_mailbox_current(uuid) TO control_plane_runtime;

-- Общий typed snapshot взят из согласованного shared health source048. EMAIL hook
-- допускает только NONE и добавляет текущий mailbox owner snapshot до штатного digest.
-- Прежние NONE/TOKEN/BI ветки и раннее закрытие terminal rows сохраняются без изменений.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.capture_integration_health_input() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $shared_health_capture$
DECLARE
 snapshot jsonb;
 base_snapshot jsonb;
 token_snapshot jsonb;
 source_broker_id uuid;
 source_token_id uuid;
 source_policy text;
 identity_purpose text;
 identity_ref text;
 identity_revision bigint;
 identity_target text;
 service_ref text;
 service_active boolean;
 configuration jsonb;
 mailbox_input jsonb;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.input_snapshot,NEW.credential_kind,NEW.broker_purpose,NEW.broker_identity_revision_id,NEW.input_digest)
   IS DISTINCT FROM ROW(OLD.input_snapshot,OLD.credential_kind,OLD.broker_purpose,OLD.broker_identity_revision_id,OLD.input_digest) THEN
   RAISE EXCEPTION 'health input snapshot is immutable';
  END IF;
  -- Closing выполняется после source/actor revoke без изменения retained evidence.
  IF NEW.state NOT IN('CLAIMED','SUCCEEDED') THEN RETURN NEW; END IF;
 END IF;
 SELECT jsonb_build_object(
  'connectionRef',c.ref,'connectionVersion',c.version,'configuration',c.public_configuration,
  'definitionKey',c.definition_key,'definitionVersion',c.definition_version,'definitionDigest',c.definition_digest),
  jsonb_build_object('credentialRevisionRef',COALESCE(cr.ref,''),'credentialRevision',COALESCE(cr.revision,0),'credentialSHA256',COALESCE(cr.content_sha256,'')),
  c.broker_identity_revision_id,c.credential_revision_id,c.credential_policy,c.public_configuration,
  identity.purpose,identity.ref,identity.revision,identity.target_ref,subject.ref,
  COALESCE(subject.active AND subject.kind='SERVICE' AND identity.binding_id IS NULL AND identity.credential_policy='PROJECT_SHARED',false)
 INTO base_snapshot,token_snapshot,source_broker_id,source_token_id,source_policy,configuration,
  identity_purpose,identity_ref,identity_revision,identity_target,service_ref,service_active
 FROM control_plane.integration_connections c
 LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
 LEFT JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=c.broker_identity_revision_id
  AND identity.organization_id=c.organization_id AND identity.connection_id=c.id
 LEFT JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=c.organization_id
 WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id;
 IF base_snapshot IS NULL THEN RAISE EXCEPTION 'health snapshot owner is missing'; END IF;
 IF NEW.credential_kind IN('NONE','TOKEN') THEN
  IF NEW.broker_purpose IS NOT NULL OR NEW.broker_identity_revision_id IS NOT NULL OR source_broker_id IS NOT NULL THEN
   RAISE EXCEPTION 'health credential source is inconsistent';
  END IF;
  snapshot:=base_snapshot||token_snapshot;
 ELSIF NEW.credential_kind='BROKER_IDENTITY' THEN
  IF source_policy<>'PROJECT_SHARED' OR source_token_id IS NOT NULL OR source_broker_id IS DISTINCT FROM NEW.broker_identity_revision_id
   OR identity_ref IS NULL OR identity_purpose IS DISTINCT FROM NEW.broker_purpose OR NOT service_active
   OR identity_target IS DISTINCT FROM configuration->>'target_ref' OR service_ref IS DISTINCT FROM configuration->>'service_subject_ref'
   OR NEW.broker_purpose NOT IN('KUBERNETES_TARGET','SSH_TARGET') THEN
   RAISE EXCEPTION 'health broker identity source is inconsistent';
  END IF;
  snapshot:=base_snapshot||jsonb_build_object('credentialDeliveryKind','BROKER_IDENTITY','brokerIdentityPurpose',identity_purpose,
   'brokerIdentityRef',identity_ref,'brokerIdentityRevision',identity_revision,'brokerIdentityTargetRef',identity_target,'serviceSubjectRef',service_ref);
 ELSE
  RAISE EXCEPTION 'health credential representation must be assigned by owner';
 END IF;
 IF base_snapshot->>'definitionKey'='email' THEN
  IF NEW.credential_kind IS DISTINCT FROM 'NONE' THEN
   RAISE EXCEPTION 'email health credential representation is invalid';
  END IF;
  mailbox_input:=control_plane.email_health_input_snapshot(NEW.organization_id,NEW.connection_id,NEW.created_by);
  IF mailbox_input IS NULL THEN RAISE EXCEPTION 'email health mailbox ownership is unavailable'; END IF;
  snapshot:=snapshot||mailbox_input;
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.input_snapshot<>'{}'::jsonb OR NEW.input_digest IS NOT NULL THEN RAISE EXCEPTION 'health snapshot must be assigned by owner'; END IF;
  NEW.input_snapshot:=snapshot;
  NEW.input_digest:=encode(sha256(convert_to(snapshot::text,'UTF8')),'hex');
 ELSE
  IF NEW.input_snapshot IS DISTINCT FROM snapshot OR NEW.input_digest IS DISTINCT FROM encode(sha256(convert_to(snapshot::text,'UTF8')),'hex') THEN
   RAISE EXCEPTION 'health input snapshot is stale';
  END IF;
 END IF;
 RETURN NEW;
END $shared_health_capture$;
-- +goose StatementEnd

-- Точный actual catalog normal8bf: stage049; future graph variants не принимаются.
-- +goose StatementBegin
DO $purge_graph_049$
DECLARE definition text; parts text[]; total_nodes integer; nodes_digest text; total_edges integer; edges_digest text;
BEGIN
 IF(SELECT md5(prosrc) FROM pg_proc WHERE oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure)<>'ed1ae9fbb4335115b2aa7d1a46ae3b71' THEN
  RAISE EXCEPTION 'project purge stage049 predecessor function changed'; END IF;
 WITH RECURSIVE nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT bound.conrelid FROM nodes JOIN pg_constraint bound ON bound.confrelid=nodes.id
   WHERE bound.contype='f' AND bound.connamespace='control_plane'::regnamespace
 ) SELECT count(*),md5(string_agg(namespace.nspname||'.'||relation.relname,E'\n' ORDER BY namespace.nspname,relation.relname))
 INTO total_nodes,nodes_digest FROM nodes JOIN pg_class relation ON relation.oid=nodes.id JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace;
 WITH RECURSIVE nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT bound.conrelid FROM nodes JOIN pg_constraint bound ON bound.confrelid=nodes.id
   WHERE bound.contype='f' AND bound.connamespace='control_plane'::regnamespace
 ) SELECT count(*),md5(string_agg(child_namespace.nspname||'.'||child.relname||'|'||bound.conname||'|'||parent_namespace.nspname||'.'||parent.relname||'|'||bound.confdeltype::text,
  E'\n' ORDER BY child_namespace.nspname,child.relname,bound.conname)) INTO total_edges,edges_digest
 FROM pg_constraint bound JOIN pg_class child ON child.oid=bound.conrelid JOIN pg_namespace child_namespace ON child_namespace.oid=child.relnamespace
 JOIN pg_class parent ON parent.oid=bound.confrelid JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent.relnamespace
 WHERE bound.contype='f' AND bound.conrelid IN(SELECT id FROM nodes) AND bound.confrelid IN(SELECT id FROM nodes);
 IF ROW(total_nodes,nodes_digest,total_edges,edges_digest) IS DISTINCT FROM ROW(139,'fa3830df53c1264f778a60a4f269c5b8',376,'512cddddde0e7b818ebcfe40f4719f3d') THEN
  RAISE EXCEPTION 'project purge stage049 exact graph changed'; END IF;
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 definition:=replace(definition,'IF v_nodes <> 137 OR v_node_fingerprint <> ''324b4e7d2f5dec55cbac7c767fd93dee'' THEN','IF v_nodes <> 139 OR v_node_fingerprint <> ''fa3830df53c1264f778a60a4f269c5b8'' THEN');
 definition:=replace(definition,'IF v_edges <> 373 OR v_edge_fingerprint <> ''ee978bed4c1cb4b555b9985c52ebca76'' THEN','IF v_edges <> 376 OR v_edge_fingerprint <> ''512cddddde0e7b818ebcfe40f4719f3d'' THEN');
 IF position($oldorder$ELSE 10 END,target.relation_id$oldorder$ IN definition)=0 THEN RAISE EXCEPTION 'email purge deletion cursor changed'; END IF;
 definition:=replace(definition,$oldorder$ELSE 10 END,target.relation_id$oldorder$,$neworder$WHEN 'control_plane.email_approval_scope_bindings'::regclass::oid THEN 10
          WHEN 'control_plane.email_invocation_mailbox_bindings'::regclass::oid THEN 11
          ELSE 12 END,target.relation_id$neworder$);
 EXECUTE definition;
END $purge_graph_049$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
  RAISE EXCEPTION 'personal email mailbox migration is forward-only';
END $$;
-- +goose StatementEnd
