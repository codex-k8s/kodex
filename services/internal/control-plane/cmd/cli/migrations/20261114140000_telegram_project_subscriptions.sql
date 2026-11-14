-- +goose Up
-- Подписка служебного бота принадлежит проекту; прежние Mattermost EACH gates не изменяются.
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $adapter_successor$
DECLARE item record; source text;
BEGIN
 SELECT conname,pg_get_constraintdef(oid) AS definition INTO STRICT item
 FROM pg_constraint WHERE conrelid='control_plane.integration_definitions'::regclass
 AND conname='integration_definitions_adapter_check' AND contype='c';
 source:=item.definition;
 IF (length(source)-length(replace(source,'''OBSERVABILITY''::text','')))/length('''OBSERVABILITY''::text')<>1
  OR position('TELEGRAM_INTERACTION' IN source)>0 THEN
  RAISE EXCEPTION 'telegram adapter predecessor changed';
 END IF;
 EXECUTE format('ALTER TABLE control_plane.integration_definitions DROP CONSTRAINT %I, ADD CONSTRAINT %I %s',
 item.conname,item.conname,replace(source,'''OBSERVABILITY''::text','''OBSERVABILITY''::text, ''TELEGRAM_INTERACTION''::text'));
END $adapter_successor$;
-- +goose StatementEnd

CREATE TABLE control_plane.telegram_subscriptions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK (ref ~ '^tgs_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 project_id uuid NOT NULL UNIQUE REFERENCES control_plane.projects(id) ON DELETE CASCADE,
 connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
 connection_version bigint NOT NULL CHECK (connection_version>0),
 credential_revision_id uuid NOT NULL REFERENCES control_plane.integration_credential_revisions(id),
 definition_version text NOT NULL,
 definition_digest text NOT NULL CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
 configuration jsonb NOT NULL CHECK (jsonb_typeof(configuration)='object' AND octet_length(configuration::text)<=4096),
 event_types text[] NOT NULL CHECK (cardinality(event_types) BETWEEN 1 AND 3 AND event_types <@ ARRAY['FINISHED','FAILED','HUMAN_GATE']::text[]),
 scope_digest text NOT NULL CHECK (scope_digest ~ '^[a-f0-9]{64}$'),
 grant_id uuid REFERENCES control_plane.integration_grants(id),
 enabled boolean NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 approved_by uuid NOT NULL REFERENCES control_plane.subjects(id),
 expires_at timestamptz NOT NULL,
 max_effects integer NOT NULL CHECK (max_effects BETWEEN 1 AND 10000),
 used_effects integer NOT NULL DEFAULT 0 CHECK (used_effects BETWEEN 0 AND max_effects),
 rate_window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 rate_window_effects integer NOT NULL DEFAULT 0 CHECK (rate_window_effects BETWEEN 0 AND 20),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK (expires_at>created_at)
);

ALTER TABLE control_plane.integration_grants
 DROP CONSTRAINT integration_grants_target_kind_check,
 ADD CONSTRAINT integration_grants_target_kind_check CHECK (target_kind IN ('AGENT','WORKFLOW','TELEGRAM_SUBSCRIPTION')),
 ADD CONSTRAINT integration_grants_telegram_kind_check CHECK (
   (target_kind='TELEGRAM_SUBSCRIPTION' AND capability_key='telegram.notifications' AND risk='WRITE'
     AND approval_policy='HUMAN_SCOPED' AND resource_kind='HTTPS_RESOURCE' AND cardinality(approval_scope_paths)=0)
   OR (target_kind<>'TELEGRAM_SUBSCRIPTION' AND capability_key NOT LIKE 'telegram.%'));

ALTER TABLE control_plane.interaction_deliveries
 DROP CONSTRAINT interaction_deliveries_capability_key_check,
 ADD CONSTRAINT interaction_deliveries_capability_key_check CHECK (capability_key IN (
   'mattermost.notifications','mattermost.result_mirror','mattermost.gate_decisions','mattermost.acknowledgements','telegram.notifications')),
 ADD COLUMN telegram_subscription_id uuid REFERENCES control_plane.telegram_subscriptions(id) ON DELETE CASCADE,
 ADD COLUMN telegram_subscription_version bigint,
 ADD COLUMN telegram_scope_digest text,
 ADD COLUMN telegram_credential_revision_id uuid REFERENCES control_plane.integration_credential_revisions(id),
 ADD COLUMN telegram_input jsonb,
 ADD COLUMN telegram_input_digest text,
 ADD COLUMN telegram_source_key text,
 ADD COLUMN telegram_gate_id uuid REFERENCES control_plane.owner_gates(id),
 ADD COLUMN telegram_effect_started_at timestamptz,
 ADD COLUMN telegram_expires_at timestamptz,
 ADD COLUMN telegram_receipt jsonb,
 ADD CONSTRAINT interaction_delivery_telegram_pin CHECK (
  (capability_key='telegram.notifications' AND telegram_subscription_id IS NOT NULL AND telegram_subscription_version>0
   AND telegram_scope_digest ~ '^[a-f0-9]{64}$' AND telegram_credential_revision_id IS NOT NULL
   AND jsonb_typeof(telegram_input)='object' AND octet_length(telegram_input::text)<=8192
   AND telegram_input_digest ~ '^[a-f0-9]{64}$' AND length(telegram_source_key) BETWEEN 1 AND 160
   AND telegram_expires_at IS NOT NULL AND approval_gate_id IS NULL AND acceptance_receipt_id IS NULL) IS TRUE
  OR (capability_key<>'telegram.notifications' AND telegram_subscription_id IS NULL AND telegram_subscription_version IS NULL
   AND telegram_scope_digest IS NULL AND telegram_credential_revision_id IS NULL AND telegram_input IS NULL
   AND telegram_input_digest IS NULL AND telegram_source_key IS NULL AND telegram_effect_started_at IS NULL
   AND telegram_gate_id IS NULL AND telegram_expires_at IS NULL AND telegram_receipt IS NULL));

DROP INDEX control_plane.interaction_deliveries_effect;
CREATE UNIQUE INDEX interaction_deliveries_effect ON control_plane.interaction_deliveries
 (connection_id,capability_key,root_run_id,COALESCE(gate_id,'00000000-0000-0000-0000-000000000000'::uuid))
 WHERE acceptance_receipt_id IS NULL AND capability_key<>'telegram.notifications';

CREATE UNIQUE INDEX interaction_deliveries_telegram_source
 ON control_plane.interaction_deliveries(telegram_subscription_id,telegram_source_key)
 WHERE telegram_subscription_id IS NOT NULL;
CREATE INDEX telegram_subscriptions_active ON control_plane.telegram_subscriptions(organization_id,project_id)
 WHERE enabled;
GRANT SELECT,INSERT,UPDATE ON control_plane.telegram_subscriptions TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.telegram_subscription_eligible(subscription_id uuid,run_id uuid,gate_id uuid,event_type text)
RETURNS boolean LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $eligible$
 SELECT EXISTS (
  SELECT 1 FROM control_plane.telegram_subscriptions subscription
  JOIN control_plane.projects project ON project.id=subscription.project_id AND project.organization_id=subscription.organization_id
  JOIN control_plane.subjects actor ON actor.id=subscription.approved_by AND actor.active
  JOIN control_plane.integration_connections connection ON connection.id=subscription.connection_id
   AND connection.organization_id=subscription.organization_id AND connection.definition_key='telegram'
   AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='CONNECTED'
   AND connection.credential_policy='PROJECT_SHARED' AND control_plane.integration_connection_configuration_ready(connection.id)
   AND connection.version=subscription.connection_version AND connection.definition_version=subscription.definition_version
   AND connection.definition_digest=subscription.definition_digest AND connection.credential_revision_id=subscription.credential_revision_id
   AND connection.public_configuration=subscription.configuration
  JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
   AND credential.organization_id=subscription.organization_id AND credential.connection_id=connection.id
  JOIN control_plane.integration_grants binding ON binding.id=subscription.grant_id
   AND binding.organization_id=subscription.organization_id AND binding.connection_id=connection.id
   AND binding.target_kind='TELEGRAM_SUBSCRIPTION' AND binding.target_ref=subscription.ref AND binding.enabled
   AND binding.capability_key='telegram.notifications' AND binding.approval_policy='HUMAN_SCOPED'
   AND binding.resource_scope_digest=subscription.scope_digest
   AND binding.definition_version=subscription.definition_version AND binding.definition_digest=subscription.definition_digest
  JOIN control_plane.runs run ON run.id=run_id AND run.organization_id=subscription.organization_id
   AND run.project_id=project.id AND run.id=run.root_run_id
  LEFT JOIN control_plane.owner_gates gate ON gate.id=gate_id AND gate.organization_id=subscription.organization_id
   AND gate.project_id=project.id AND gate.root_run_id=run.id
  WHERE subscription.id=subscription_id AND subscription.enabled AND subscription.expires_at>clock_timestamp()
   AND subscription.used_effects<subscription.max_effects AND event_type=ANY(subscription.event_types)
   AND project.lifecycle='ACTIVE'
   AND ((event_type='FINISHED' AND gate_id IS NULL AND run.state='SUCCEEDED')
    OR (event_type='FAILED' AND gate_id IS NULL AND run.state='FAILED')
    OR (event_type='HUMAN_GATE' AND gate.state='OPEN' AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELLING')))
   AND control_plane.catalog_resource_visible(project.organization_id,actor.id,'project.manage','PROJECT',
    project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
   AND control_plane.catalog_resource_visible(connection.organization_id,actor.id,'integration.manage','INTEGRATION',
    connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
   AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
    WHERE target.organization_id=run.organization_id AND target.kind='RUN' AND target.id=run.id
    AND control_plane.catalog_resource_visible(run.organization_id,actor.id,'run.view',target.kind,
     target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp()))
 );
$eligible$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.telegram_subscription_eligible(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.telegram_subscription_eligible(uuid,uuid,uuid,text) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_telegram_subscription() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $guard$
DECLARE subscription control_plane.telegram_subscriptions%ROWTYPE;
BEGIN
 IF TG_TABLE_NAME='telegram_subscriptions' THEN
  IF TG_OP='UPDATE' AND (NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.created_at)
   IS DISTINCT FROM (OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.created_at) THEN
   RAISE EXCEPTION 'telegram subscription ownership is immutable';
  END IF;
  SELECT * INTO subscription FROM control_plane.telegram_subscriptions WHERE id=NEW.id;
 ELSE
  IF TG_OP='UPDATE' AND OLD.target_kind='TELEGRAM_SUBSCRIPTION'
   AND (NEW.organization_id,NEW.connection_id,NEW.capability_key,NEW.target_kind,NEW.target_ref)
    IS DISTINCT FROM (OLD.organization_id,OLD.connection_id,OLD.capability_key,OLD.target_kind,OLD.target_ref) THEN
   RAISE EXCEPTION 'telegram grant ownership is immutable';
  END IF;
  IF NEW.target_kind<>'TELEGRAM_SUBSCRIPTION' THEN RETURN NULL; END IF;
  SELECT * INTO subscription FROM control_plane.telegram_subscriptions
   WHERE organization_id=NEW.organization_id AND ref=NEW.target_ref;
  IF NOT FOUND THEN RAISE EXCEPTION 'telegram grant requires its exact subscription owner'; END IF;
 END IF;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.projects project JOIN control_plane.integration_connections connection
   ON connection.id=subscription.connection_id AND connection.organization_id=project.organization_id
  JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
   AND credential.organization_id=subscription.organization_id AND credential.connection_id=connection.id
  WHERE project.id=subscription.project_id AND project.organization_id=subscription.organization_id
   AND connection.definition_key='telegram' AND (NOT subscription.enabled OR connection.credential_policy='PROJECT_SHARED')) THEN
  RAISE EXCEPTION 'telegram subscription tenant and service credential pins are invalid';
 END IF;
 IF subscription.enabled AND NOT EXISTS(SELECT 1 FROM control_plane.integration_grants binding
  WHERE binding.id=subscription.grant_id AND binding.organization_id=subscription.organization_id
   AND binding.connection_id=subscription.connection_id AND binding.target_kind='TELEGRAM_SUBSCRIPTION'
   AND binding.target_ref=subscription.ref AND binding.capability_key='telegram.notifications' AND binding.enabled
   AND binding.resource_scope_digest=subscription.scope_digest AND binding.definition_version=subscription.definition_version
   AND binding.definition_digest=subscription.definition_digest) THEN
  RAISE EXCEPTION 'telegram subscription requires its exact bounded grant';
 END IF;
 RETURN NULL;
END $guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_telegram_subscription() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER telegram_subscription_owner_guard AFTER INSERT OR UPDATE ON control_plane.telegram_subscriptions
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_telegram_subscription();
CREATE CONSTRAINT TRIGGER telegram_grant_owner_guard AFTER INSERT OR UPDATE ON control_plane.integration_grants
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_telegram_subscription();

-- +goose StatementBegin
CREATE FUNCTION control_plane.revoke_telegram_subscriptions() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $revoke$
DECLARE target_connection uuid; target_project uuid; closed_gate uuid;
BEGIN
 IF TG_TABLE_NAME='integration_connections' THEN
  IF (NEW.version,NEW.enabled,NEW.lifecycle_state,NEW.credential_policy,NEW.credential_revision_id,NEW.definition_version,NEW.definition_digest,NEW.public_configuration)
   IS NOT DISTINCT FROM (OLD.version,OLD.enabled,OLD.lifecycle_state,OLD.credential_policy,OLD.credential_revision_id,OLD.definition_version,OLD.definition_digest,OLD.public_configuration) THEN RETURN NEW; END IF;
  UPDATE control_plane.telegram_subscriptions SET enabled=false,version=version+1,updated_at=clock_timestamp()
   WHERE connection_id=NEW.id AND enabled;
  target_connection:=NEW.id;
 ELSIF TG_TABLE_NAME='projects' THEN
  IF NEW.lifecycle IS NOT DISTINCT FROM OLD.lifecycle OR NEW.lifecycle='ACTIVE' THEN RETURN NEW; END IF;
  UPDATE control_plane.telegram_subscriptions SET enabled=false,version=version+1,updated_at=clock_timestamp()
   WHERE project_id=NEW.id AND enabled;
  target_project:=NEW.id;
 ELSIF TG_TABLE_NAME='owner_gates' THEN
  IF NEW.state='OPEN' OR NEW.state IS NOT DISTINCT FROM OLD.state THEN RETURN NEW; END IF;
  closed_gate:=NEW.id;
 END IF;
 UPDATE control_plane.integration_grants binding SET enabled=false,version=binding.version+1,updated_at=clock_timestamp()
 FROM control_plane.telegram_subscriptions subscription
 WHERE binding.id=subscription.grant_id AND NOT subscription.enabled AND binding.enabled
  AND (subscription.connection_id=target_connection OR subscription.project_id=target_project);
 UPDATE control_plane.interaction_deliveries delivery
 SET state=CASE WHEN delivery.telegram_effect_started_at IS NULL THEN 'CANCELLED' ELSE 'UNKNOWN_OUTCOME' END,
  safe_error_code=CASE WHEN delivery.telegram_effect_started_at IS NULL THEN 'TELEGRAM_SCOPE_REVOKED' ELSE 'TELEGRAM_OUTCOME_UNKNOWN' END,
  lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
  completed_at=clock_timestamp(),updated_at=clock_timestamp(),version=delivery.version+1
 FROM control_plane.telegram_subscriptions subscription
 WHERE delivery.telegram_subscription_id=subscription.id AND delivery.state IN ('DUE','FAILED','CLAIMED')
  AND ((NOT subscription.enabled AND (subscription.connection_id=target_connection OR subscription.project_id=target_project))
   OR delivery.telegram_gate_id=closed_gate);
 RETURN NEW;
END $revoke$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.revoke_telegram_subscriptions() FROM PUBLIC;
CREATE TRIGGER telegram_connection_revoke AFTER UPDATE ON control_plane.integration_connections
 FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_telegram_subscriptions();
CREATE TRIGGER telegram_project_revoke AFTER UPDATE ON control_plane.projects
 FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_telegram_subscriptions();
CREATE TRIGGER telegram_gate_revoke AFTER UPDATE ON control_plane.owner_gates
 FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_telegram_subscriptions();
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$ BEGIN
 RAISE EXCEPTION 'telegram subscription schema is forward-only';
END $forward_only$;
-- +goose StatementEnd
