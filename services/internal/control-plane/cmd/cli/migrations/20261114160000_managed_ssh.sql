-- +goose Up
-- Сохраняем снимки и итоговые записи миграции047. Источник новых проверок назначает владелец.
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
SET LOCAL lock_timeout='8s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
LOCK TABLE control_plane.integration_connection_tests IN ACCESS EXCLUSIVE MODE;

-- Закреплённый источник проверки общего подключения.
-- +goose StatementBegin
DO $shared_health_drain$
BEGIN
 IF EXISTS(SELECT 1 FROM control_plane.integration_connection_tests WHERE state IN('DUE','CLAIMED')) THEN
  RAISE EXCEPTION 'typed shared health activation requires no active tests';
 END IF;
END $shared_health_drain$;
-- +goose StatementEnd
ALTER TABLE control_plane.integration_connection_tests
 ADD COLUMN credential_kind text,
 ADD COLUMN broker_purpose text,
 ADD COLUMN broker_identity_revision_id uuid,
 ADD COLUMN input_digest text,
 ADD CONSTRAINT integration_connection_tests_broker_identity_fk FOREIGN KEY(organization_id,connection_id,broker_identity_revision_id)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,connection_id,id),
 ADD CONSTRAINT integration_connection_tests_credential_source_oneof CHECK(COALESCE(
  (credential_kind IS NULL AND broker_purpose IS NULL AND broker_identity_revision_id IS NULL AND input_digest IS NULL)
  OR (credential_kind IN('NONE','TOKEN') AND broker_purpose IS NULL AND broker_identity_revision_id IS NULL AND input_digest~'^[a-f0-9]{64}$')
  OR (credential_kind='BROKER_IDENTITY' AND broker_purpose IN('KUBERNETES_TARGET','SSH_TARGET') AND broker_identity_revision_id IS NOT NULL AND input_digest~'^[a-f0-9]{64}$')
 ,false)),
 ADD CONSTRAINT integration_connection_tests_active_typed_source CHECK(state NOT IN('DUE','CLAIMED') OR credential_kind IS NOT NULL),
 ADD CONSTRAINT integration_connection_tests_broker_one_attempt CHECK(credential_kind IS DISTINCT FROM 'BROKER_IDENTITY' OR
  (purpose='OWNER_TEST' AND attempt=1 AND generation BETWEEN 0 AND 1 AND (state<>'DUE' OR generation=0) AND (state<>'CLAIMED' OR generation=1)));

-- Исторические строки сохраняют NULL/unknown representation и original snapshot bytes.
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

-- Общая проверка Kubernetes использует отдельную привязку проверки.
ALTER TABLE control_plane.integration_connection_tests
 ADD CONSTRAINT integration_connection_tests_health_org_unique UNIQUE(organization_id,id);
ALTER TABLE control_plane.kubernetes_identity_health_attempts
 ALTER COLUMN personal_test_id DROP NOT NULL,
 ADD COLUMN shared_test_id uuid UNIQUE,
 ADD CONSTRAINT kubernetes_health_shared_test_org_fk FOREIGN KEY(organization_id,shared_test_id)
  REFERENCES control_plane.integration_connection_tests(organization_id,id) DEFERRABLE INITIALLY IMMEDIATE,
 ADD CONSTRAINT kubernetes_health_test_source_oneof CHECK(num_nonnulls(personal_test_id,shared_test_id)=1);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_kubernetes_identity_health() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $health_guard$
BEGIN
 IF TG_TABLE_NAME='kubernetes_identity_health_attempts' THEN
  IF NEW.personal_test_id IS NOT NULL THEN
  IF NOT EXISTS(SELECT 1 FROM control_plane.integration_personal_credential_tests test
   JOIN control_plane.integration_personal_credentials binding ON binding.id=test.binding_id
    AND binding.organization_id=test.organization_id AND binding.subject_id=test.subject_id
   JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=NEW.identity_revision_id
    AND identity.organization_id=test.organization_id AND identity.binding_id=binding.id
    AND identity.subject_id=test.subject_id AND identity.connection_id=test.connection_id
   JOIN control_plane.kubernetes_operator_current current_source ON current_source.singleton
    AND current_source.revision=NEW.registry_revision
   JOIN control_plane.kubernetes_target_revisions target ON target.registry_revision<=NEW.registry_revision
    AND target.ref=NEW.target_ref AND target.revision=NEW.target_revision
   WHERE test.id=NEW.personal_test_id AND test.organization_id=NEW.organization_id
    AND test.state='CLAIMED' AND test.credential_kind='BROKER_IDENTITY' AND test.broker_purpose='KUBERNETES_TARGET'
    AND test.broker_identity_revision_id=identity.id AND binding.broker_identity_revision_id=identity.id
    AND binding.state='CHECKING' AND binding.credential_kind='BROKER_IDENTITY' AND binding.broker_purpose='KUBERNETES_TARGET'
    AND identity.purpose='KUBERNETES_TARGET' AND identity.target_ref=NEW.target_ref
    AND test.lease_ref=NEW.lease_ref AND test.fence_digest=NEW.fence_digest AND test.generation=NEW.generation
    AND test.workload_instance=NEW.workload_instance AND test.lease_expires_at=NEW.expires_at
    AND test.lease_expires_at>clock_timestamp()
  ) THEN RAISE EXCEPTION 'Managed Kubernetes health admission is not current'; END IF;
  ELSE
   IF NOT EXISTS(SELECT 1 FROM control_plane.integration_connection_tests test
    JOIN control_plane.integration_connections connection ON connection.id=test.connection_id AND connection.organization_id=test.organization_id
    JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=NEW.identity_revision_id
     AND identity.organization_id=test.organization_id AND identity.connection_id=test.connection_id
     AND identity.binding_id IS NULL AND identity.credential_policy='PROJECT_SHARED' AND identity.purpose='KUBERNETES_TARGET'
    JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=identity.organization_id
     AND subject.kind='SERVICE' AND subject.active AND subject.ref=connection.public_configuration->>'service_subject_ref'
    JOIN control_plane.kubernetes_operator_current current_source ON current_source.singleton AND current_source.revision=NEW.registry_revision
    JOIN control_plane.kubernetes_target_revisions target ON target.registry_revision<=NEW.registry_revision
     AND target.ref=NEW.target_ref AND target.revision=NEW.target_revision
    WHERE test.id=NEW.shared_test_id AND test.organization_id=NEW.organization_id
     AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='TESTING'
     AND connection.credential_policy='PROJECT_SHARED' AND connection.credential_revision_id IS NULL
     AND connection.broker_identity_revision_id=identity.id AND identity.target_ref=NEW.target_ref
     AND test.state='CLAIMED' AND test.credential_kind='BROKER_IDENTITY' AND test.broker_purpose='KUBERNETES_TARGET'
     AND test.broker_identity_revision_id=identity.id AND test.purpose='OWNER_TEST' AND test.attempt=1
     AND test.lease_ref=NEW.lease_ref AND test.fence_digest=NEW.fence_digest AND test.generation=NEW.generation
     AND test.workload_instance=NEW.workload_instance AND test.claimed_workload='integration-gateway'
     AND test.lease_expires_at=NEW.expires_at AND test.lease_expires_at>clock_timestamp()
     AND test.input_digest=NEW.input_digest
     AND test.input_digest=encode(sha256(convert_to(test.input_snapshot::text,'UTF8')),'hex')
   ) THEN RAISE EXCEPTION 'Managed Kubernetes shared health admission is not current'; END IF;
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_health_attempts attempt
   LEFT JOIN control_plane.integration_personal_credential_tests test ON test.id=attempt.personal_test_id
    AND test.organization_id=attempt.organization_id
   LEFT JOIN control_plane.integration_connection_tests shared_test ON shared_test.id=attempt.shared_test_id AND shared_test.organization_id=attempt.organization_id
   WHERE attempt.id=NEW.attempt_id AND (
    (NOT NEW.applied AND NEW.state='CANCELLED') OR
    (NEW.applied AND attempt.expires_at>clock_timestamp() AND coalesce(test.state,shared_test.state)=NEW.state AND
     ((NEW.outcome='AUTHENTICATED' AND NEW.state='SUCCEEDED') OR (NEW.outcome<>'AUTHENTICATED' AND NEW.state='FAILED')))
   )
  ) THEN RAISE EXCEPTION 'Managed Kubernetes health completion is not closed'; END IF;
 END IF;
 RETURN NEW;
END $health_guard$;
-- +goose StatementEnd

-- Настройка SSH, проверка подключения и журнал попыток исполнения.
-- +goose StatementBegin
DO $adapter_successor$
DECLARE source text; marker constant text := '''KUBERNETES''::text';
BEGIN
 SELECT pg_get_constraintdef(oid) INTO STRICT source FROM pg_constraint WHERE conrelid='control_plane.integration_definitions'::regclass
  AND conname='integration_definitions_adapter_check' AND contype='c' AND convalidated;
 IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1 OR position('SSH_TARGET' IN source)>0 THEN RAISE EXCEPTION 'managed SSH adapter predecessor changed';END IF;
 EXECUTE format('ALTER TABLE control_plane.integration_definitions DROP CONSTRAINT integration_definitions_adapter_check, ADD CONSTRAINT integration_definitions_adapter_check %s',replace(source,marker,marker||', ''SSH_TARGET''::text'));
END $adapter_successor$;
-- +goose StatementEnd
CREATE TABLE control_plane.ssh_identity_setups (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), ref text NOT NULL UNIQUE CHECK(ref~'^ssu_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL, connection_id uuid NOT NULL, actor_id uuid NOT NULL, identity_revision_id uuid NOT NULL UNIQUE,
 material_namespace text NOT NULL CHECK(material_namespace='kodex-ssh-identities'),
 expected_connection_version bigint NOT NULL CHECK(expected_connection_version>0),
 expected_binding_version bigint NOT NULL CHECK(expected_binding_version>=0),
 expected_binding_generation bigint NOT NULL CHECK(expected_binding_generation>=0),
 definition_digest text NOT NULL CHECK(definition_digest~'^[a-f0-9]{64}$'),
 configuration_digest text NOT NULL CHECK(configuration_digest~'^[a-f0-9]{64}$'),
 begin_idempotency_key text NOT NULL CHECK(length(begin_idempotency_key) BETWEEN 8 AND 128),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),
 permit_sha256 text NOT NULL CHECK(permit_sha256~'^[a-f0-9]{64}$'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[a-f0-9]{64}$'), snapshot_body text NOT NULL CHECK(octet_length(snapshot_body)<=8192),
 content_sha256 text NOT NULL CHECK(content_sha256~'^[a-f0-9]{64}$'), content_bytes integer NOT NULL CHECK(content_bytes BETWEEN 1 AND 16384),
 authentication_sha256 text CHECK(authentication_sha256~'^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL, closed_at timestamptz,
 state text NOT NULL CHECK(state IN('PREPARED','ATTEMPTING','UNKNOWN','UNREPORTED','ACK_REJECTED','COMPLETED','CANCELLED','EXPIRED','QUARANTINED')),
 reason_code text NOT NULL, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 UNIQUE(organization_id,id), UNIQUE(organization_id,actor_id,begin_idempotency_key),
 FOREIGN KEY(organization_id,connection_id) REFERENCES control_plane.integration_connections(organization_id,id),
 FOREIGN KEY(organization_id,actor_id) REFERENCES control_plane.subjects(organization_id,id),
 FOREIGN KEY(organization_id,connection_id,identity_revision_id) REFERENCES control_plane.integration_broker_identity_revisions(organization_id,connection_id,id),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '90 seconds'),
 CHECK((state IN('PREPARED','ATTEMPTING','UNKNOWN'))=(closed_at IS NULL))
);
CREATE TABLE control_plane.ssh_identity_upload_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), ref text NOT NULL UNIQUE CHECK(ref~'^ssa_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL, setup_id uuid NOT NULL, generation bigint NOT NULL CHECK(generation BETWEEN 1 AND 8),
 mode text NOT NULL CHECK(mode IN('WRITE','OBSERVE')), workload_instance text NOT NULL CHECK(length(workload_instance) BETWEEN 8 AND 96),
 fence_sha256 text NOT NULL CHECK(fence_sha256~'^[a-f0-9]{64}$'), snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[a-f0-9]{64}$'),
 state text NOT NULL CHECK(state IN('ACTIVE','CLOSED')), created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 closed_at timestamptz, close_reason text,
 UNIQUE(organization_id,id), UNIQUE(organization_id,setup_id,id), UNIQUE(setup_id,generation),
 FOREIGN KEY(organization_id,setup_id) REFERENCES control_plane.ssh_identity_setups(organization_id,id),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 seconds'),
 CHECK((state='ACTIVE')=(closed_at IS NULL AND close_reason IS NULL))
);
CREATE UNIQUE INDEX ssh_identity_one_write ON control_plane.ssh_identity_upload_attempts(setup_id) WHERE mode='WRITE';
CREATE UNIQUE INDEX ssh_identity_one_active ON control_plane.ssh_identity_upload_attempts(setup_id) WHERE state='ACTIVE';
CREATE TABLE control_plane.ssh_identity_upload_observations (
 organization_id uuid NOT NULL, setup_id uuid NOT NULL, attempt_id uuid NOT NULL,
 outcome text NOT NULL CHECK(outcome IN('KNOWN','UNKNOWN','ACK_REJECTED')),
 receipt_sha256 text NOT NULL CHECK(receipt_sha256~'^[a-f0-9]{64}$'), receipt_body text NOT NULL CHECK(octet_length(receipt_body)<=8192),
 recorded_at timestamptz NOT NULL, PRIMARY KEY(attempt_id,outcome,receipt_sha256),
 FOREIGN KEY(organization_id,setup_id,attempt_id) REFERENCES control_plane.ssh_identity_upload_attempts(organization_id,setup_id,id)
);
CREATE TABLE control_plane.ssh_identity_material_revisions (
 organization_id uuid NOT NULL, connection_id uuid NOT NULL, identity_revision_id uuid PRIMARY KEY,
 setup_id uuid NOT NULL, attempt_id uuid NOT NULL, namespace text NOT NULL CHECK(namespace='kodex-ssh-identities'),
 secret_name text NOT NULL CHECK(secret_name~'^sshbi-[a-f0-9]{32}-r[1-9][0-9]*$'), secret_uid text NOT NULL CHECK(length(secret_uid) BETWEEN 1 AND 256),
 secret_resource_version text NOT NULL CHECK(length(secret_resource_version) BETWEEN 1 AND 256),
 content_sha256 text NOT NULL CHECK(content_sha256~'^[a-f0-9]{64}$'), authentication_sha256 text NOT NULL CHECK(authentication_sha256~'^[a-f0-9]{64}$'), recorded_at timestamptz NOT NULL,
 FOREIGN KEY(organization_id,connection_id,identity_revision_id) REFERENCES control_plane.integration_broker_identity_revisions(organization_id,connection_id,id),
 FOREIGN KEY(organization_id,setup_id,attempt_id) REFERENCES control_plane.ssh_identity_upload_attempts(organization_id,setup_id,id)
);
CREATE TABLE control_plane.ssh_identity_ack_rejections (
 organization_id uuid NOT NULL, setup_id uuid NOT NULL, attempt_id uuid NOT NULL, identity_revision_id uuid NOT NULL,
 response_sha256 text NOT NULL CHECK(response_sha256~'^[a-f0-9]{64}$'), truncated boolean NOT NULL, observed_uid text NOT NULL CHECK(length(observed_uid)<=256),
 recorded_at timestamptz NOT NULL, PRIMARY KEY(attempt_id,response_sha256,truncated,observed_uid),
 FOREIGN KEY(organization_id,setup_id,attempt_id) REFERENCES control_plane.ssh_identity_upload_attempts(organization_id,setup_id,id),
 FOREIGN KEY(organization_id,identity_revision_id) REFERENCES control_plane.integration_broker_identity_revisions(organization_id,id)
);
CREATE TABLE control_plane.ssh_identity_quarantines (
 organization_id uuid NOT NULL, identity_revision_id uuid PRIMARY KEY, setup_id uuid NOT NULL,
 reason_code text NOT NULL CHECK(reason_code='CONTRADICTORY_MATERIAL'), recorded_at timestamptz NOT NULL,
 FOREIGN KEY(organization_id,identity_revision_id) REFERENCES control_plane.integration_broker_identity_revisions(organization_id,id),
 FOREIGN KEY(organization_id,setup_id) REFERENCES control_plane.ssh_identity_setups(organization_id,id)
);
CREATE TRIGGER ssh_identity_observations_immutable BEFORE UPDATE OR DELETE ON control_plane.ssh_identity_upload_observations FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER ssh_identity_material_immutable BEFORE UPDATE OR DELETE ON control_plane.ssh_identity_material_revisions FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER ssh_identity_rejections_immutable BEFORE UPDATE OR DELETE ON control_plane.ssh_identity_ack_rejections FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER ssh_identity_quarantines_immutable BEFORE UPDATE OR DELETE ON control_plane.ssh_identity_quarantines FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.ssh_identity_setups,control_plane.ssh_identity_upload_attempts FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.ssh_identity_setups,control_plane.ssh_identity_upload_attempts TO control_plane_runtime;
REVOKE ALL ON control_plane.ssh_identity_upload_observations,control_plane.ssh_identity_material_revisions,control_plane.ssh_identity_ack_rejections,control_plane.ssh_identity_quarantines FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.ssh_identity_upload_observations,control_plane.ssh_identity_material_revisions,control_plane.ssh_identity_ack_rejections,control_plane.ssh_identity_quarantines TO control_plane_runtime;

-- Owner health credit отдельно от upload: expiry прежнего setup не отменяет Known material.
ALTER TABLE control_plane.integration_personal_credential_tests ADD CONSTRAINT ssh_health_personal_owner UNIQUE(organization_id,connection_id,id);
ALTER TABLE control_plane.integration_connection_tests ADD CONSTRAINT ssh_health_shared_owner UNIQUE(organization_id,connection_id,id);
CREATE TABLE control_plane.ssh_identity_health_probes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),ref text NOT NULL UNIQUE,
 organization_id uuid NOT NULL,connection_id uuid NOT NULL,identity_revision_id uuid NOT NULL,
 personal_test_id uuid,shared_test_id uuid,
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256~'^[a-f0-9]{64}$'),
 definition_digest text NOT NULL CHECK(definition_digest~'^[a-f0-9]{64}$'),
 configuration_digest text NOT NULL CHECK(configuration_digest~'^[a-f0-9]{64}$'),
 transport_mode text NOT NULL CHECK(transport_mode IN('MANAGED','EXECUTE')),
 input_digest text NOT NULL CHECK(input_digest~'^[a-f0-9]{64}$'),
 lease_ref text NOT NULL,fence_digest text NOT NULL,workload_instance text NOT NULL,generation bigint NOT NULL CHECK(generation=1),
 expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(expires_at>created_at),
 outcome text CHECK(outcome IN('AUTHENTICATED','AUTH_REJECTED','UNAVAILABLE')),
 observed_subject_digest text,qualified_until timestamptz,terminal_state text CHECK(terminal_state IN('SUCCEEDED','FAILED','CANCELLED')),applied boolean,
 CONSTRAINT ssh_health_personal_once UNIQUE(organization_id,personal_test_id),CONSTRAINT ssh_health_shared_once UNIQUE(organization_id,shared_test_id),
 CHECK(num_nonnulls(personal_test_id,shared_test_id)=1),
 CHECK((outcome IS NULL AND observed_subject_digest IS NULL AND qualified_until IS NULL AND terminal_state IS NULL AND applied IS NULL)
  OR COALESCE(outcome IS NOT NULL AND terminal_state IS NOT NULL AND applied IS NOT NULL AND
   ((outcome='AUTHENTICATED' AND observed_subject_digest~'^[a-f0-9]{64}$' AND qualified_until IS NOT NULL)
    OR (outcome<>'AUTHENTICATED' AND observed_subject_digest IS NULL AND qualified_until IS NULL)),false)),
 FOREIGN KEY(organization_id,connection_id,identity_revision_id) REFERENCES control_plane.integration_broker_identity_revisions(organization_id,connection_id,id),
 FOREIGN KEY(organization_id,connection_id,personal_test_id) REFERENCES control_plane.integration_personal_credential_tests(organization_id,connection_id,id),
 FOREIGN KEY(organization_id,connection_id,shared_test_id) REFERENCES control_plane.integration_connection_tests(organization_id,connection_id,id)
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_health_receipt() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $guard$
BEGIN
 IF TG_OP='DELETE' OR OLD.outcome IS NOT NULL OR
  (to_jsonb(NEW)-ARRAY['outcome','observed_subject_digest','qualified_until','terminal_state','applied']) IS DISTINCT FROM
  (to_jsonb(OLD)-ARRAY['outcome','observed_subject_digest','qualified_until','terminal_state','applied']) THEN
  RAISE EXCEPTION 'SSH health receipt is immutable';
 END IF;
 RETURN NEW;
END $guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_ssh_health_receipt() FROM PUBLIC,control_plane_runtime;
CREATE TRIGGER ssh_health_receipt_immutable BEFORE UPDATE OR DELETE ON control_plane.ssh_identity_health_probes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_health_receipt();
REVOKE ALL ON control_plane.ssh_identity_health_probes FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.ssh_identity_health_probes TO control_plane_runtime;

-- Удаление исторической записи допускается только для точной цели штатного purge.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_project_receipt_delete() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $ssh_project_delete$
BEGIN
 IF TG_OP='DELETE' AND TG_NARGS=1 AND control_plane.project_purge_authorized() AND EXISTS(
  SELECT 1 FROM control_plane.project_purge_targets target
  JOIN control_plane.project_purge_context context USING(transaction_id)
  WHERE target.relation_id=TG_RELID AND target.row_key=jsonb_build_array(to_jsonb(OLD)->TG_ARGV[0])
   AND context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
 ) THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'SSH project receipt is immutable' USING ERRCODE='23514';
END $ssh_project_delete$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_ssh_project_receipt_delete() FROM PUBLIC,control_plane_runtime;

-- Immutable invocation lineage отделена от execution grant и не содержит private locator.
CREATE TABLE control_plane.ssh_invocation_identity_bindings (
 invocation_id uuid PRIMARY KEY REFERENCES control_plane.integration_invocations(id),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
 root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
 subject_id uuid NOT NULL REFERENCES control_plane.subjects(id),binding_id uuid REFERENCES control_plane.integration_personal_credentials(id),
 binding_generation bigint NOT NULL CHECK(binding_generation>=0),identity_revision_id uuid NOT NULL REFERENCES control_plane.ssh_identity_material_revisions(identity_revision_id),
 health_probe_id uuid NOT NULL REFERENCES control_plane.ssh_identity_health_probes(id),
 subject_digest text NOT NULL CHECK(subject_digest~'^[a-f0-9]{64}$'),valid_until timestamptz NOT NULL,
 definition_digest text NOT NULL CHECK(definition_digest~'^[a-f0-9]{64}$'),configuration_digest text NOT NULL CHECK(configuration_digest~'^[a-f0-9]{64}$'),
 transport_mode text NOT NULL CHECK(transport_mode IN('MANAGED','EXECUTE')),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((binding_id IS NOT NULL AND binding_generation>0 AND subject_id=root_actor_id) OR (binding_id IS NULL AND binding_generation=0))
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.ssh_invocation_identity_current(pin control_plane.ssh_invocation_identity_bindings) RETURNS boolean
LANGUAGE sql VOLATILE SET search_path=pg_catalog,control_plane AS $ssh_source$
 WITH stamp AS MATERIALIZED (SELECT clock_timestamp() AS at)
 SELECT EXISTS(
  SELECT 1 FROM control_plane.integration_invocations invocation
  JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
  JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
  JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id AND root.root_run_id=root.id
  JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
  JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=pin.identity_revision_id
   AND identity.organization_id=invocation.organization_id AND identity.connection_id=connection.id AND identity.purpose='SSH_TARGET'
   AND identity.credential_policy=connection.credential_policy AND identity.subject_id=pin.subject_id
   AND identity.target_ref=connection.public_configuration->>'target_ref'
  JOIN control_plane.ssh_identity_material_revisions material ON material.identity_revision_id=identity.id
   AND material.organization_id=identity.organization_id AND material.connection_id=connection.id
  JOIN control_plane.ssh_identity_upload_observations observation ON observation.attempt_id=material.attempt_id
   AND observation.organization_id=identity.organization_id AND observation.outcome='KNOWN'
  JOIN control_plane.ssh_identity_health_probes health ON health.id=pin.health_probe_id AND health.organization_id=identity.organization_id
   AND health.connection_id=connection.id AND health.identity_revision_id=identity.id AND health.outcome='AUTHENTICATED'
   AND health.terminal_state='SUCCEEDED' AND health.applied AND health.observed_subject_digest=pin.subject_digest
   AND health.definition_digest=pin.definition_digest AND health.configuration_digest=pin.configuration_digest AND health.transport_mode=pin.transport_mode
  LEFT JOIN control_plane.integration_personal_credentials binding ON binding.id=pin.binding_id AND binding.organization_id=identity.organization_id
   AND binding.connection_id=connection.id AND binding.subject_id=identity.subject_id
  LEFT JOIN control_plane.integration_personal_credential_tests personal ON personal.id=health.personal_test_id AND personal.organization_id=identity.organization_id
  LEFT JOIN control_plane.integration_connection_tests shared ON shared.id=health.shared_test_id AND shared.organization_id=identity.organization_id
  CROSS JOIN stamp
  WHERE invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id AND connection.id=pin.connection_id
   AND root.id=pin.root_run_id AND root.initiated_by=pin.root_actor_id AND root.state IN('RUNNING','WAITING_HUMAN')
   AND invocation.state IN('READY','WAITING_APPROVAL','RUNNING') AND control_plane.integration_connection_configuration_ready(connection.id)
   AND connection.credential_revision_id IS NULL AND connection.definition_digest=pin.definition_digest AND invocation.definition_digest=pin.definition_digest
   AND ((connection.definition_key='ssh-managed' AND pin.transport_mode='MANAGED') OR (connection.definition_key='ssh-execute' AND pin.transport_mode='EXECUTE'))
   AND pin.valid_until>stamp.at AND pin.valid_until<=least(health.qualified_until,health.created_at+interval '5 minutes')
   AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_ack_rejections rejection WHERE rejection.identity_revision_id=identity.id)
   AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_quarantines quarantine WHERE quarantine.identity_revision_id=identity.id)
   AND ((connection.credential_policy='INITIATOR_REQUIRED' AND actor.kind='USER' AND pin.subject_id=actor.id
     AND NOT connection.public_configuration ? 'service_subject_ref' AND identity.binding_id=binding.id
     AND binding.generation=pin.binding_generation AND binding.state='READY' AND binding.credential_kind='BROKER_IDENTITY'
     AND binding.broker_purpose='SSH_TARGET' AND binding.broker_identity_revision_id=identity.id AND binding.verified_broker_identity_revision_id=identity.id
     AND binding.credential_revision_id IS NULL AND binding.verified_credential_revision_id IS NULL
     AND binding.verified_definition_digest=pin.definition_digest AND binding.verified_configuration_digest=pin.configuration_digest
     AND (binding.expires_at IS NULL OR binding.expires_at>stamp.at)
     AND personal.binding_id=binding.id AND personal.state='SUCCEEDED'
     AND personal.input_snapshot->>'DefinitionDigest'=pin.definition_digest AND personal.input_snapshot->>'ConfigurationDigest'=pin.configuration_digest
     AND (personal.input_snapshot->>'BindingGeneration')::bigint=pin.binding_generation)
    OR (connection.credential_policy='PROJECT_SHARED' AND connection.state='CONNECTED' AND pin.binding_id IS NULL AND pin.binding_generation=0 AND identity.binding_id IS NULL
     AND connection.broker_identity_revision_id=identity.id AND shared.state='SUCCEEDED' AND shared.broker_identity_revision_id=identity.id
     AND shared.input_snapshot->>'definitionDigest'=pin.definition_digest AND shared.input_snapshot->'configuration'=connection.public_configuration
     AND (shared.input_snapshot->>'connectionVersion')::bigint=connection.version-1
     AND EXISTS(SELECT 1 FROM control_plane.subjects service WHERE service.id=pin.subject_id AND service.organization_id=identity.organization_id
      AND service.kind='SERVICE' AND service.active AND service.ref=connection.public_configuration->>'service_subject_ref')))
 );
$ssh_source$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.ssh_invocation_identity_current(control_plane.ssh_invocation_identity_bindings) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.ssh_invocation_identity_current(control_plane.ssh_invocation_identity_bindings) TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_invocation_identity() RETURNS trigger LANGUAGE plpgsql
SET search_path=pg_catalog,control_plane AS $ssh_guard$
BEGIN
 IF NOT control_plane.ssh_invocation_identity_current(NEW) OR NOT EXISTS(
  SELECT 1 FROM control_plane.integration_invocations invocation WHERE invocation.id=NEW.invocation_id
   AND invocation.organization_id=NEW.organization_id AND invocation.state IN('READY','WAITING_APPROVAL')
 ) THEN RAISE EXCEPTION 'SSH invocation source is not current'; END IF;
 RETURN NEW;
END $ssh_guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_ssh_invocation_identity() FROM PUBLIC,control_plane_runtime;
CREATE TRIGGER ssh_invocation_identity_owner BEFORE INSERT ON control_plane.ssh_invocation_identity_bindings
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_invocation_identity();
CREATE TRIGGER ssh_invocation_identity_immutable BEFORE UPDATE ON control_plane.ssh_invocation_identity_bindings
 FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER ssh_invocation_identity_purge_only BEFORE DELETE ON control_plane.ssh_invocation_identity_bindings
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_project_receipt_delete('invocation_id');
CREATE INDEX ssh_invocation_identity_binding_scope ON control_plane.ssh_invocation_identity_bindings(organization_id,connection_id,binding_id);
CREATE INDEX ssh_invocation_identity_revision_scope ON control_plane.ssh_invocation_identity_bindings(organization_id,identity_revision_id);
REVOKE ALL ON control_plane.ssh_invocation_identity_bindings FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.ssh_invocation_identity_bindings TO control_plane_runtime;

CREATE TABLE control_plane.ssh_invocation_claim_cursors (
 organization_id uuid PRIMARY KEY REFERENCES control_plane.organizations(id),
 after_created_at timestamptz NOT NULL DEFAULT 'epoch',after_ref text NOT NULL DEFAULT ''
);
REVOKE ALL ON control_plane.ssh_invocation_claim_cursors FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.ssh_invocation_claim_cursors TO control_plane_runtime;
CREATE TABLE control_plane.ssh_execution_intents (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),ref text NOT NULL UNIQUE CHECK(ref~'^sxe_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 invocation_id uuid NOT NULL UNIQUE REFERENCES control_plane.integration_invocations(id),
 identity_revision_id uuid NOT NULL REFERENCES control_plane.ssh_identity_material_revisions(identity_revision_id),
 workload_instance text NOT NULL,lease_ref text NOT NULL,fence_digest text NOT NULL CHECK(fence_digest~'^[a-f0-9]{64}$'),
 generation bigint NOT NULL CHECK(generation>0),input_digest text NOT NULL CHECK(input_digest~'^[a-f0-9]{64}$'),
 snapshot_body text NOT NULL CHECK(octet_length(snapshot_body)<=131072),snapshot_digest text NOT NULL CHECK(snapshot_digest~'^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 seconds'),
 CHECK(encode(sha256(convert_to(snapshot_body,'UTF8')),'hex')=snapshot_digest)
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_execution_intent() RETURNS trigger LANGUAGE plpgsql
SET search_path=pg_catalog,control_plane AS $ssh_execution$
BEGIN
 IF NOT EXISTS(
  SELECT 1 FROM control_plane.integration_invocations invocation
  JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
  WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
   AND pin.identity_revision_id=NEW.identity_revision_id AND control_plane.ssh_invocation_identity_current(pin)
   AND invocation.state='RUNNING' AND invocation.claimed_workload='integration-gateway'
   AND invocation.workload_instance=NEW.workload_instance AND invocation.lease_ref=NEW.lease_ref
   AND invocation.effect_fence_digest=NEW.fence_digest AND invocation.generation=NEW.generation AND invocation.input_digest=NEW.input_digest
   AND invocation.lease_expires_at>=NEW.expires_at AND pin.valid_until>=NEW.expires_at
   AND (invocation.approval_policy<>'HUMAN_SCOPED' OR EXISTS(
    SELECT 1 FROM control_plane.integration_approval_scopes approval
    WHERE approval.id=invocation.approval_scope_id AND approval.organization_id=invocation.organization_id
     AND approval.revoked_at IS NULL AND approval.expires_at>=NEW.expires_at))
   AND control_plane.runtime_execution_before_deadline(invocation.organization_id,invocation.run_id)
   AND NEW.created_at<=clock_timestamp() AND NEW.expires_at>clock_timestamp()
 ) THEN RAISE EXCEPTION 'SSH execution intent source is not current'; END IF;
 RETURN NEW;
END $ssh_execution$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_ssh_execution_intent() FROM PUBLIC,control_plane_runtime;
CREATE TRIGGER ssh_execution_intent_owner BEFORE INSERT ON control_plane.ssh_execution_intents
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_execution_intent();
CREATE TRIGGER ssh_execution_intent_immutable BEFORE UPDATE ON control_plane.ssh_execution_intents
 FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER ssh_execution_intent_purge_only BEFORE DELETE ON control_plane.ssh_execution_intents
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_project_receipt_delete('id');
CREATE TABLE control_plane.ssh_execution_observations (
 intent_id uuid PRIMARY KEY REFERENCES control_plane.ssh_execution_intents(id),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 outcome text NOT NULL CHECK(outcome IN('SUCCEEDED','FAILED','UNKNOWN')),
 result_body text NOT NULL DEFAULT '' CHECK(octet_length(result_body)<=65536),
 result_digest text NOT NULL DEFAULT '',applied boolean NOT NULL,
 terminal_state text NOT NULL CHECK(terminal_state IN('SUCCEEDED','FAILED','UNKNOWN_OUTCOME','CANCELLED','DISCARDED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((result_body='' AND result_digest='' AND outcome<>'SUCCEEDED') OR
  (result_body<>'' AND result_digest~'^[a-f0-9]{64}$' AND outcome<>'UNKNOWN' AND encode(sha256(convert_to(result_body,'UTF8')),'hex')=result_digest)),
 CHECK((outcome='UNKNOWN' AND terminal_state='UNKNOWN_OUTCOME') OR outcome<>'UNKNOWN' OR NOT applied)
);
CREATE TRIGGER ssh_execution_observation_immutable BEFORE UPDATE ON control_plane.ssh_execution_observations
 FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER ssh_execution_observation_purge_only BEFORE DELETE ON control_plane.ssh_execution_observations
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_project_receipt_delete('intent_id');
REVOKE ALL ON control_plane.ssh_execution_intents,control_plane.ssh_execution_observations FROM PUBLIC,control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.ssh_execution_intents,control_plane.ssh_execution_observations TO control_plane_runtime;

-- Ровно четыре новых SSH FK принадлежат существующему project purge graph.
-- Обычные statements проверяются сразу; только явный SET CONSTRAINTS может
-- отложить проверку до commit, как в canonical project_purge_deferred_graph.
-- +goose StatementBegin
DO $ssh_project_deferred$
DECLARE exact_count integer; remaining integer;
BEGIN
 WITH expected(child,name,parent,definition) AS (VALUES
  ('control_plane.ssh_invocation_identity_bindings'::regclass::oid,'ssh_invocation_identity_bindings_invocation_id_fkey','control_plane.integration_invocations'::regclass::oid,'FOREIGN KEY (invocation_id) REFERENCES control_plane.integration_invocations(id)'),
  ('control_plane.ssh_invocation_identity_bindings'::regclass::oid,'ssh_invocation_identity_bindings_root_run_id_fkey','control_plane.runs'::regclass::oid,'FOREIGN KEY (root_run_id) REFERENCES control_plane.runs(id)'),
  ('control_plane.ssh_execution_intents'::regclass::oid,'ssh_execution_intents_invocation_id_fkey','control_plane.integration_invocations'::regclass::oid,'FOREIGN KEY (invocation_id) REFERENCES control_plane.integration_invocations(id)'),
  ('control_plane.ssh_execution_observations'::regclass::oid,'ssh_execution_observations_intent_id_fkey','control_plane.ssh_execution_intents'::regclass::oid,'FOREIGN KEY (intent_id) REFERENCES control_plane.ssh_execution_intents(id)')
 ) SELECT count(*) INTO exact_count FROM expected
 JOIN pg_constraint bound ON bound.conrelid=expected.child AND bound.conname=expected.name AND bound.confrelid=expected.parent
 WHERE bound.contype='f' AND bound.confdeltype='a' AND bound.confupdtype='a' AND NOT bound.condeferrable AND NOT bound.condeferred
  AND pg_get_constraintdef(bound.oid)=expected.definition;
 IF exact_count<>4 THEN RAISE EXCEPTION 'SSH project deferred foreign-key predecessor changed'; END IF;
 ALTER TABLE control_plane.ssh_invocation_identity_bindings ALTER CONSTRAINT ssh_invocation_identity_bindings_invocation_id_fkey DEFERRABLE INITIALLY IMMEDIATE;
 ALTER TABLE control_plane.ssh_invocation_identity_bindings ALTER CONSTRAINT ssh_invocation_identity_bindings_root_run_id_fkey DEFERRABLE INITIALLY IMMEDIATE;
 ALTER TABLE control_plane.ssh_execution_intents ALTER CONSTRAINT ssh_execution_intents_invocation_id_fkey DEFERRABLE INITIALLY IMMEDIATE;
 ALTER TABLE control_plane.ssh_execution_observations ALTER CONSTRAINT ssh_execution_observations_intent_id_fkey DEFERRABLE INITIALLY IMMEDIATE;
 WITH RECURSIVE nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT bound.conrelid FROM nodes JOIN pg_constraint bound ON bound.confrelid=nodes.id
   WHERE bound.contype='f' AND bound.connamespace='control_plane'::regnamespace
 ) SELECT count(*) INTO remaining FROM pg_constraint bound WHERE bound.contype='f' AND bound.confdeltype='a' AND NOT bound.condeferrable
  AND bound.conrelid IN(SELECT id FROM nodes) AND bound.confrelid IN(SELECT id FROM nodes);
 IF remaining<>0 THEN RAISE EXCEPTION 'SSH project deferred foreign-key graph is incomplete'; END IF;
END $ssh_project_deferred$;
-- +goose StatementEnd

-- Точный actual catalog normal8bf: stage048; future graph variants не принимаются.
-- +goose StatementBegin
DO $purge_graph_048$
DECLARE definition text; parts text[]; total_nodes integer; nodes_digest text; total_edges integer; edges_digest text;
BEGIN
 IF(SELECT md5(prosrc) FROM pg_proc WHERE oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure)<>'1b19ebd2d20323071e8d28129314e25a' THEN
  RAISE EXCEPTION 'project purge stage048 predecessor function changed'; END IF;
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
 IF ROW(total_nodes,nodes_digest,total_edges,edges_digest) IS DISTINCT FROM ROW(137,'324b4e7d2f5dec55cbac7c767fd93dee',373,'ee978bed4c1cb4b555b9985c52ebca76') THEN
  RAISE EXCEPTION 'project purge stage048 exact graph changed'; END IF;
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 definition:=replace(definition,'IF v_nodes <> 134 OR v_node_fingerprint <> ''ac4d979eccde3cb554e3818eb6e7445f'' THEN','IF v_nodes <> 137 OR v_node_fingerprint <> ''324b4e7d2f5dec55cbac7c767fd93dee'' THEN');
 definition:=replace(definition,'IF v_edges <> 369 OR v_edge_fingerprint <> ''c9a212a3f1bda48841abdc05eb573365'' THEN','IF v_edges <> 373 OR v_edge_fingerprint <> ''ee978bed4c1cb4b555b9985c52ebca76'' THEN');
 IF position($oldorder$ELSE 7 END,target.relation_id$oldorder$ IN definition)=0 THEN RAISE EXCEPTION 'SSH purge deletion cursor changed'; END IF;
 definition:=replace(definition,$oldorder$ELSE 7 END,target.relation_id$oldorder$,$neworder$WHEN 'control_plane.ssh_execution_observations'::regclass::oid THEN 7
          WHEN 'control_plane.ssh_execution_intents'::regclass::oid THEN 8
          WHEN 'control_plane.ssh_invocation_identity_bindings'::regclass::oid THEN 9
          ELSE 10 END,target.relation_id$neworder$);
 EXECUTE definition;
END $purge_graph_048$;
-- +goose StatementEnd

-- Version ledger принадлежит Goose и находится в public.
SET LOCAL search_path=public;
RESET ROLE;
